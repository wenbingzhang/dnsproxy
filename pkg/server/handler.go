package server

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/karlseguin/ccache"
	"github.com/miekg/dns"
	"github.com/wenbingzhang/dnsproxy/pkg/config"
	"github.com/wenbingzhang/dnsproxy/pkg/log"
)

type Middleware func(question dns.Question, netProtocol string) ([]dns.RR, error)

type Hostfile interface {
	FindHosts(name string) ([]net.IP, error)
	FindReverse(name string) (string, error)
}

type DnsHandler struct {
	Config     *config.Config
	hosts      Hostfile
	rcache     *ccache.Cache
	version    string
	middleware Middleware
	sync.WaitGroup
}

func (handler *DnsHandler) ServeDNS(w dns.ResponseWriter, req *dns.Msg) {
	startTime := time.Now()
	defer func() {
		elapsed := time.Since(startTime)
		log.Logger.Debugf("[%d] Response time: %s", req.Id, elapsed)
	}()
	log.Logger.Debugf("====================== [%d] ======================", req.Id)
	log.Logger.Debugf("reqest query from %s\n%s", w.RemoteAddr().String(), req.String())

	handler.Add(1)
	defer handler.Done()

	questionDomain := req.Question[0].Name

	authorities, err := handler.LeadAuthority(questionDomain)
	if err != nil {
		log.Logger.Errorf("unable to find authority for %s : %s", questionDomain, err)
		return
	}

	protocol := getNetProtocol(w)
	dnsResp, err := handler.ResolveDnsQuery(req, authorities, protocol)
	if err != nil {
		log.Logger.Errorf("unable to find resolve dns query for %s : %s", questionDomain, err)
		return
	}

	log.Logger.Debugf("response query from %s\n%s", w.RemoteAddr().String(), dnsResp.String())

	if err := w.WriteMsg(dnsResp); err != nil {
		log.Logger.Errorf("unable to write msg %s", err)
	}
}

func (handler *DnsHandler) LeadAuthority(url string) ([]config.Authority, error) {
	results := []config.Authority{}
	for _, authority := range handler.Config.Authorities {
		if authority.DomainName != "" && strings.HasSuffix(url, authority.DomainName) {
			results = append(results, authority)
		}
	}

	if len(results) == 0 {
		for _, authority := range handler.Config.Authorities {
			if authority.DomainName == "" {
				results = append(results, authority)
			}
		}
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("unable to find authority for %s", url)
	}

	return results, nil
}

func (handler *DnsHandler) ResolveDnsQuery(req *dns.Msg, authorities []config.Authority, netProtocol string) (*dns.Msg, error) {
	dnsResp := new(dns.Msg)
	dnsResp.SetReply(req)

	for _, question := range req.Question {
		name := strings.ToLower(question.Name)

		// middleware
		if handler.middleware != nil {
			if records, err := handler.middleware(question, netProtocol); err == nil && len(records) > 0 {
				dnsResp.Answer = append(dnsResp.Answer, records...)
				continue
			}
		}

		// Check hosts records before forwarding the query
		if question.Qtype == dns.TypeA || question.Qtype == dns.TypeAAAA || question.Qtype == dns.TypeANY {
			records, err := handler.AddressRecords(question, name)
			if err != nil {
				log.Logger.Errorf("Error looking up hostsfile records: %s", err)
			}
			if len(records) > 0 {
				log.Logger.Debugf("[%d] Found name in hostsfile records", req.Id)
				dnsResp.Answer = append(dnsResp.Answer, records...)
				continue
			}
		}

		if question.Qtype == dns.TypePTR && strings.HasSuffix(name, ".in-addr.arpa.") || strings.HasSuffix(name, ".ip6.arpa.") {
			if records, err := handler.PTRRecords(question); err == nil && len(records) > 0 {
				dnsResp.Answer = append(dnsResp.Answer, records...)
				continue
			}
		}

		if question.Qclass == dns.ClassCHAOS {
			dnsResp.Authoritative = true
			if question.Qtype == dns.TypeTXT {
				switch name {
				case "version.bind.":
					fallthrough
				case "version.Server.":
					hdr := dns.RR_Header{Name: question.Name, Rrtype: dns.TypeTXT, Class: dns.ClassCHAOS, Ttl: 0}
					dnsResp.Answer = append(dnsResp.Answer, &dns.TXT{Hdr: hdr, Txt: []string{handler.version}})
				case "hostname.bind.":
					fallthrough
				case "id.Server.":
					// TODO(miek): machine name to return
					hdr := dns.RR_Header{Name: question.Name, Rrtype: dns.TypeTXT, Class: dns.ClassCHAOS, Ttl: 0}
					dnsResp.Answer = append(dnsResp.Answer, &dns.TXT{Hdr: hdr, Txt: []string{"localhost"}})
				}
			}
			continue
		}

		item, err := handler.rcache.Fetch("question:"+netProtocol+":"+question.String(), handler.Config.Cache.TTL, func() (interface{}, error) {
			reqCopy := req.Copy()
			reqCopy.Question = []dns.Question{
				question,
			}
			reqCopy.Answer = []dns.RR{}
			reqCopy.RecursionDesired = false
			reqCopy.SetEdns0(dns.DefaultMsgSize, true)

			resp, _, err := handler.exchange(reqCopy, authorities, netProtocol)
			if err != nil {
				return nil, fmt.Errorf("unable to get info msg %s", err)
			}
			return resp, nil

		})
		if err != nil {
			log.Logger.Errorf("%s", err)
			dnsResp.SetRcode(dnsResp, dns.RcodeNameError)
			break
		}
		tmpResp := item.Value().(*dns.Msg)
		dnsResp.Answer = append(dnsResp.Answer, tmpResp.Answer...)
		// dnsResp.Ns = append(dnsResp.Answer, tmpResp.Ns...)
		dnsResp.Extra = append(dnsResp.Extra, tmpResp.Extra...)

	}
	return dnsResp, nil
}

func (handler *DnsHandler) exchange(req *dns.Msg, authorities []config.Authority, netProtocol string) (resp *dns.Msg, rtt time.Duration, err error) {
	nsIdx := 0
	maxTry := len(authorities)
	// log.Logger.Debugf("[%d] request message: %s", req.Id, req.String())
	for try := 1; try <= maxTry+1; try++ {
		authority := authorities[nsIdx]

		client := &dns.Client{Net: netProtocol, Timeout: authority.Timeout, SingleInflight: true}
		resp, _, err = client.Exchange(req, authority.Address)

		// log.Logger.Debugf("[%d] response message flags: QR=%t AA=%t RA=%t RCODE=%d", resp.Id, resp.Response, resp.Authoritative, resp.RecursionAvailable, resp.Rcode)
		if err == nil {
			log.Logger.Debugf("[%d] response code from upstream: %s", req.Id, dns.RcodeToString[resp.Rcode])
			switch resp.Rcode {
			// SUCCESS
			case dns.RcodeSuccess:
				fallthrough
			case dns.RcodeNameError:
				fallthrough
			// NO RECOVERY
			case dns.RcodeFormatError:
				fallthrough
			case dns.RcodeRefused:
				fallthrough
			case dns.RcodeNotImplemented:
				return
			}
		}

		// Continue with next available Server
		if maxTry-1 > nsIdx {
			nsIdx++
		} else {
			nsIdx = 0
		}
	}
	return
}

func (handler *DnsHandler) AddressRecords(q dns.Question, name string) (records []dns.RR, err error) {
	results, err := handler.hosts.FindHosts(name)
	if err != nil {
		return nil, err
	}

	for _, ip := range results {
		switch {
		case ip.To4() != nil && (q.Qtype == dns.TypeA || q.Qtype == dns.TypeANY):
			r := new(dns.A)
			r.Hdr = dns.RR_Header{Name: q.Name, Rrtype: dns.TypeA,
				Class: dns.ClassINET, Ttl: 10}
			r.A = ip.To4()
			records = append(records, r)
		case ip.To4() == nil && (q.Qtype == dns.TypeAAAA || q.Qtype == dns.TypeANY):
			r := new(dns.AAAA)
			r.Hdr = dns.RR_Header{Name: q.Name, Rrtype: dns.TypeAAAA,
				Class: dns.ClassINET, Ttl: 10}
			r.AAAA = ip.To16()
			records = append(records, r)
		}
	}
	return records, nil
}

func (handler *DnsHandler) PTRRecords(q dns.Question) (records []dns.RR, err error) {
	name := strings.ToLower(q.Name)
	result, err := handler.hosts.FindReverse(name)
	if err != nil {
		return nil, err
	}
	if result != "" {
		r := new(dns.PTR)
		r.Hdr = dns.RR_Header{Name: q.Name, Rrtype: dns.TypePTR,
			Class: dns.ClassINET, Ttl: 10}
		r.Ptr = result
		records = append(records, r)
	}
	return records, nil
}
