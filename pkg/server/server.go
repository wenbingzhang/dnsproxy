package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/karlseguin/ccache"
	"github.com/miekg/dns"
	"github.com/wenbingzhang/dnsproxy/pkg/config"
	"github.com/wenbingzhang/dnsproxy/pkg/hosts"
	"github.com/wenbingzhang/dnsproxy/pkg/log"
	"golang.org/x/sync/errgroup"
)

type Server struct {
	handler *DnsHandler
}

func New(cfg config.Config) (*Server, error) {
	if cfg.Cache.Max <= 0 {
		cfg.Cache.Max = 5000
	}
	if cfg.Cache.TTL <= 0 {
		cfg.Cache.TTL = 15 * time.Second
	}

	hosts, err := hosts.NewHostsfile(&cfg.HostConfig)
	if err != nil {
		return nil, err
	}
	return &Server{
		handler: &DnsHandler{
			Config: &cfg,
			hosts:  hosts,
			rcache: ccache.New(ccache.Configure().MaxSize(cfg.Cache.Max)),
		},
	}, nil
}

func (s *Server) UseMiddleware(f Middleware) {
	s.handler.middleware = f
}

func (s *Server) UseLogger(logger log.ILogger) {
	log.Logger = logger
}

func (s *Server) Start() error {
	// trap Ctrl+C and call cancel on the context
	ctx, done := context.WithCancel(context.Background())
	eg, gctx := errgroup.WithContext(ctx)

	// Check Ctrl+C or Signals
	eg.Go(func() error {
		signalChannel := make(chan os.Signal, 1)
		signal.Notify(signalChannel, os.Interrupt, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

		select {
		case sig := <-signalChannel:
			log.Logger.Debugf("Received signal: %s\n", sig)
			done()
		case <-gctx.Done():
			log.Logger.Debugf("closing signal goroutine\n")
			return gctx.Err()
		}

		return nil
	})

	// Run DNS server
	eg.Go(func() error {
		errCh := make(chan error)
		go func() { errCh <- s.run(gctx) }()
		select {
		case err := <-errCh:
			log.Logger.Debug("error from errCh", err)
			return err
		case <-gctx.Done():
			return gctx.Err()
		}
	})

	if err := eg.Wait(); err != nil {
		if errors.Is(err, context.Canceled) {
			log.Logger.Info("context was canceled")
			return nil
		} else {
			log.Logger.Error("error", err)
			return err
		}
	}
	return nil
}

// run is a blocking operation that starts the Server listening on the DNS ports.
func (s *Server) run(ctx context.Context) error {
	mux := dns.NewServeMux()
	mux.Handle(".", s.handler)
	log.Logger.Debug("start as proccess")
	return s.runProccess(ctx, mux)
}

func (s *Server) runProccess(ctx context.Context, mux *dns.ServeMux) error {
	eg, ctx := errgroup.WithContext(ctx)
	eg.Go(s.dnsListenAndServerWithContext(ctx, s.handler.Config.ServerAddr, "tcp", mux))
	eg.Go(s.dnsListenAndServerWithContext(ctx, s.handler.Config.ServerAddr, "udp", mux))
	return eg.Wait()
}

func (s *Server) dnsListenAndServerWithContext(ctx context.Context, addr, net string, mux *dns.ServeMux) func() error {
	return func() error {
		server := &dns.Server{Addr: addr, Net: net, Handler: mux}
		go func() {
			<-ctx.Done()
			server.ShutdownContext(ctx)
		}()
		if err := server.ListenAndServe(); err != nil {
			return fmt.Errorf("%s %s : %w", net, addr, err)
		}
		return nil
	}
}
