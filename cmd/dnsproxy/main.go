package main

import (
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/wenbingzhang/dnsproxy/pkg/config"
	"github.com/wenbingzhang/dnsproxy/pkg/hosts"
	"github.com/wenbingzhang/dnsproxy/pkg/server"
)

var (
	logger    = log.New()
	dnsConfig = &config.Config{
		ServerAddr: "0.0.0.0:53",
		Authorities: []config.Authority{
			{
				Address: "223.5.5.5:53",
				Timeout: 2 * time.Second,
			},
			{
				Address: "223.6.6.6:53",
				Timeout: 2 * time.Second,
			},
			{
				Address: "114.114.144.114:53",
				Timeout: 2 * time.Second,
			},
			{
				Address: "8.8.8.8:53",
				Timeout: 2 * time.Second,
			},
		},
		HostConfig: hosts.Config{
			Path: "/etc/hosts",
			Poll: 10 * time.Second,
		},
	}
)

func init() {
	// 初始化日志
	logger.SetLevel(log.DebugLevel)
}

func main() {
	s, err := server.New(*dnsConfig)
	if err != nil {
		panic(err)
	}
	s.UseLogger(logger)
	s.Start()
}
