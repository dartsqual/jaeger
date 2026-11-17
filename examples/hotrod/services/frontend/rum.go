// Copyright (c) 2026 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package frontend

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
)

// rumCDNSites maps DD_SITE to the Datadog Browser SDK CDN path.
var rumCDNSites = map[string]string{
	"datadoghq.com":     "us1",
	"us3.datadoghq.com": "us3",
	"us5.datadoghq.com": "us5",
	"datadoghq.eu":      "eu1",
	"ap1.datadoghq.com": "ap1",
	"ap2.datadoghq.com": "ap2",
}

// rumSnippet builds the Datadog Browser RUM snippet from environment variables.
// It returns nil (RUM disabled) unless DD_RUM_APPLICATION_ID and
// DD_RUM_CLIENT_TOKEN are set.
//
//	DD_RUM_APPLICATION_ID, DD_RUM_CLIENT_TOKEN  required
//	DD_SITE                                     default datadoghq.com
//	DD_RUM_SERVICE                              default hotrod-web
//	DD_ENV, DD_VERSION                          optional
//	DD_RUM_SESSION_SAMPLE_RATE                  default 100
//	DD_RUM_SESSION_REPLAY_SAMPLE_RATE           default 100
//
// Same-origin requests (/dispatch, /config) get W3C traceparent headers, so the
// browser sessions are linked to the backend traces (dd-trace-go).
func rumSnippet() []byte {
	appID := os.Getenv("DD_RUM_APPLICATION_ID")
	token := os.Getenv("DD_RUM_CLIENT_TOKEN")
	if appID == "" || token == "" {
		return nil
	}
	site := getenvDefault("DD_SITE", "datadoghq.com")
	cdn, ok := rumCDNSites[site]
	if !ok {
		cdn = "us1"
	}
	cfg := map[string]any{
		"applicationId":           appID,
		"clientToken":             token,
		"site":                    site,
		"service":                 getenvDefault("DD_RUM_SERVICE", "hotrod-web"),
		"env":                     os.Getenv("DD_ENV"),
		"version":                 os.Getenv("DD_VERSION"),
		"sessionSampleRate":       getenvFloat("DD_RUM_SESSION_SAMPLE_RATE", 100),
		"sessionReplaySampleRate": getenvFloat("DD_RUM_SESSION_REPLAY_SAMPLE_RATE", 100),
		"trackResources":          true,
		"trackUserInteractions":   true,
		"trackLongTasks":          true,
		"defaultPrivacyLevel":     "mask-user-input",
	}
	// json.Marshal escapes <, > and & so the config is safe inside <script>.
	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		return nil
	}
	var b bytes.Buffer
	b.WriteString("<script>\n")
	b.WriteString("(function(h,o,u,n,d){h=h[d]=h[d]||{q:[],onReady:function(c){h.q.push(c)}};" +
		"d=o.createElement(u);d.async=1;d.src=n;d.crossOrigin='';" +
		"n=o.getElementsByTagName(u)[0];n.parentNode.insertBefore(d,n)})" +
		"(window,document,'script','https://www.datadoghq-browser-agent.com/" + cdn + "/v7/datadog-rum.js','DD_RUM');\n")
	b.WriteString("window.DD_RUM.onReady(function(){var c=")
	b.Write(cfgJSON)
	b.WriteString(";c.allowedTracingUrls=[{match:function(u){return u.indexOf(window.location.origin)===0;}," +
		"propagatorTypes:['tracecontext']}];window.DD_RUM.init(c);});\n")
	b.WriteString("</script>\n")
	return b.Bytes()
}

// withRUM serves index.html with the RUM snippet injected before </head>.
// Other assets are served unchanged. When RUM is not configured, it returns next.
func (s *Server) withRUM(next http.Handler) http.Handler {
	snippet := rumSnippet()
	if snippet == nil {
		return next
	}
	s.logger.Bg().Info("Datadog RUM enabled for the frontend")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "", "/", "index.html", "/index.html":
		default:
			next.ServeHTTP(w, r)
			return
		}
		f, err := s.assetFS.Open("/index.html")
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		defer f.Close()
		page, err := io.ReadAll(f)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		page = bytes.Replace(page, []byte("</head>"), append(snippet, []byte("</head>")...), 1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(page)
	})
}

func getenvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getenvFloat(key string, def float64) float64 {
	if v, err := strconv.ParseFloat(os.Getenv(key), 64); err == nil {
		return v
	}
	return def
}
