// SPDX-FileCopyrightText: 2026 gorfc community contributors
// SPDX-License-Identifier: Apache-2.0

package nwrfc

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"sync"
)

// IniFS is an `io/fs.FS`-shaped abstraction the wrapper uses
// to read sapnwrfc.ini-style profiles. Callers can plug in a
// custom FS (e.g. backed by Kubernetes ConfigMaps, S3, Vault)
// to source destinations without writing files to disk.
//
// Inspired by node-rfc's `customFs`. The Go shape uses the
// stdlib `io/fs.FS` so any embed.FS / os.DirFS / in-memory
// implementation just works.
type IniFS = fs.FS

var (
	iniFSMu sync.RWMutex
	iniFS   IniFS
)

// SetIniFS installs an [IniFS] as the source of sapnwrfc.ini
// content. Pass nil to revert to the SDK's default file-system
// lookup (the legacy upstream behavior). The first call to
// [OpenDest] after installation reads through the registered
// FS.
//
// The implementation parses sapnwrfc.ini Go-side and translates
// each [DEST=NAME] block into Params, then opens the connection
// directly via Open. The SDK's own ini lookup is bypassed.
func SetIniFS(f IniFS) {
	iniFSMu.Lock()
	defer iniFSMu.Unlock()
	iniFS = f
}

// resolveIniDest reads the sapnwrfc.ini-shaped content from
// the registered IniFS and returns Params for the named
// destination, or an error if the destination is missing or
// the FS misbehaves.
//
// Returns (Params{}, nil) with a sentinel "no FS registered"
// signal handled at the caller (Open / OpenDest).
func resolveIniDest(_ context.Context, name string) (Params, bool, error) {
	iniFSMu.RLock()
	f := iniFS
	iniFSMu.RUnlock()
	if f == nil {
		return Params{}, false, nil
	}
	file, err := f.Open("sapnwrfc.ini")
	if err != nil {
		return Params{}, true, fmt.Errorf("nwrfc: IniFS: %w", err)
	}
	defer file.Close()

	dests, err := parseIni(file)
	if err != nil {
		return Params{}, true, err
	}
	got, ok := dests[name]
	if !ok {
		return Params{}, true, &ConfigError{Field: "dest", Hint: "no [DEST=" + name + "] in IniFS"}
	}
	return got, true, nil
}

// parseIni parses the sapnwrfc.ini file format: blocks of
// KEY=VALUE lines separated by `DEST=NAME` headers (or blank
// lines). Returns a map keyed by DEST name.
//
// Whitespace and comment lines (#, ;) are ignored.
func parseIni(r io.Reader) (map[string]Params, error) {
	out := map[string]Params{}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	var current Params
	var currentName string

	flush := func() {
		if currentName == "" {
			return
		}
		current.Dest = currentName
		out[currentName] = current
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])

		// DEST= starts a new block.
		if strings.EqualFold(key, "DEST") {
			flush()
			currentName = val
			current = Params{Dest: val}
			continue
		}
		applyIniField(&current, key, val)
	}
	flush()
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// applyIniField copies one ini-line key/value into the
// matching Params field. Unknown keys are stashed in Extra.
func applyIniField(p *Params, key, val string) {
	switch strings.ToLower(key) {
	case "ashost":
		p.AsHost = val
	case "sysnr":
		p.SysNr = val
	case "mshost":
		p.MsHost = val
	case "r3name":
		p.R3Name = val
	case "group":
		p.Group = val
	case "wshost":
		p.WSHost = val
	case "wsport":
		p.WSPort = val
	case "client":
		p.Client = val
	case "user":
		p.User = val
	case "passwd", "password":
		p.Passwd = val
	case "lang":
		p.Lang = val
	case "mysapsso2":
		p.Mysapsso2 = val
	case "x509cert":
		p.X509Cert = val
	case "snc_qop":
		p.SncQOP = val
	case "snc_lib":
		p.SncLib = val
	case "snc_myname":
		p.SncMyName = val
	case "snc_partnername":
		p.SncPartnerName = val
	case "snc_sso":
		p.SncSso = val
	case "tls_client_pse":
		p.TLSClientPSE = val
	case "tls_trust_all":
		p.TLSTrustAll = val
	case "trace":
		p.Trace = val
	default:
		if p.Extra == nil {
			p.Extra = map[string]string{}
		}
		p.Extra[strings.ToLower(key)] = val
	}
}
