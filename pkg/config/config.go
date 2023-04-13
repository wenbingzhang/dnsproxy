package config

import (
	"time"

	"github.com/wenbingzhang/dnsproxy/pkg/hosts"
)

type Authority struct {
	Address    string
	DomainName string
	Timeout    time.Duration
}

type Cache struct {
	Max int64
	TTL time.Duration
}

type Config struct {
	ServerAddr  string
	Authorities []Authority
	Cache       Cache
	HostConfig  hosts.Config
}
