package main

import (
	"github.com/Chistovik92/hydravpn-router/internal/config"
	"github.com/Chistovik92/hydravpn-router/pkg/version"
)

// applyDebugBuild raises log levels in debug pre-releases (vX.Y.Z-debug.N).
func applyDebugBuild(s *config.Settings) {
	if !version.IsDebug() {
		return
	}
	s.LogLevel = "debug"
	s.AppLogLevel = "debug"
}
