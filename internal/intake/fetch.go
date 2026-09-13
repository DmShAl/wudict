// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

package intake

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/wuweidict/wudict/internal/dict"
)

// Fetching an archive over https.
//
// This is in Go rather than in the Android shell for two reasons. The server
// is a separate process, so it is not bound by the app's
// network_security_config, which means one code path serves every platform
// instead of a Java downloader that exists only on the phone; and a download
// that outlives the activity is exactly what an import needs on a device where
// the page dies on rotation.
//
// A URL reaches here by share, by paste, or by a tap on one of the few hosts
// the Android manifest offers to open (D130, D139). None of those is silent:
// the user handed the link over. So the default is to fetch what they asked
// for from wherever they asked for it, and IMPORT_URL_HOSTS is an opt-in
// restriction for the one case that is not personal - a wudict reachable from
// a network, where the caller pointing the download may not be the user.
//
// The refusals that protect the MACHINE are separate and are not a host list:
// plain http, and any address that resolves to loopback, a private range or a
// link-local block. Those hold whatever IMPORT_URL_HOSTS says, and only
// IMPORT_INSECURE lifts them.

// DownloadDirName is where a fetched archive lands, inside the dictionary
// folder. Deliberately NOT the staging area: staged files are intermediaries
// and are removed when the job ends however it ends, and a download is not an
// intermediary. It is the only copy of a file the user asked for, it took
// minutes to acquire, and whether it survives is their decision
// (IMPORT_KEEP) - which it cannot be if the job's teardown has already taken
// it. Being a plain visible folder is part of the same thought: a user who
// kept the bundle must be able to find it.
// The name itself is dict's, because Discover has to skip this folder and the
// two must agree about which one it is.
const DownloadDirName = dict.DownloadDirName

// partialStale is how long an unfinished download is kept for resuming.
// Much longer than a staging directory's hour, because this is the case the
// .part file EXISTS for: a phone that lost its connection is asked to finish
// tomorrow, not to fetch two gigabytes again.
const partialStale = 7 * 24 * time.Hour

// maxDownloadBytes bounds what a single URL may deliver. Not a memory bound -
// the body is written straight to disk - but a bound on how much of somebody's
// storage one mistyped link can consume.
const maxDownloadBytes = 16 << 30

// maxRedirects is the hop limit. Each hop is re-checked against the same
// scheme and host rules, so a redirect cannot be the way around them.
const maxRedirects = 8

var (
	// ErrInsecureURL is plain http, or a URL that is not http(s) at all.
	ErrInsecureURL = errors.New("only https downloads are allowed")
	// ErrHostNotAllowed is a host outside IMPORT_URL_HOSTS.
	ErrHostNotAllowed = errors.New("downloads from that site are not enabled")
	// ErrNotArchive is a URL that answered with something that is neither an
	// archive nor a dictionary file this build reads. Reported from the
	// RESPONSE HEADERS, before the body is downloaded: finding out after two
	// gigabytes that the link was an HTML login page is not a diagnosis, it is
	// a waste of somebody's data.
	ErrNotArchive = errors.New("that link is not a dictionary or an archive this app can read")

	// errNotModified is internal: the file this URL names is already on disk
	// and the server agrees it has not changed. Not an error the user sees -
	// it is the successful outcome of the cheapest possible download.
	errNotModified = errors.New("not modified")
)

// Fetcher is the download policy: where from, and how strictly.
type Fetcher struct {
	// Hosts is the optional restriction, matching a host and its subdomains.
	// Empty - the default - is no restriction: the link was the user's own
	// (D139). Set it where the caller might not be the user.
	Hosts []string
	// Insecure lifts two refusals at once: plain http, and addresses on the
	// local network. They are one question - "I trust what I am pointing this
	// at" - and splitting them would invite turning off the wrong one.
	Insecure bool
}

// DownloadDir is where fetched archives land under dest.
func DownloadDir(dest string) string { return filepath.Join(dest, DownloadDirName) }

// Check validates a URL against the policy without contacting anything, so a
// refusal is immediate and no job is claimed for a link that was never going
// to be fetched.
func (f Fetcher) Check(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return nil, errors.New("that is not a link")
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
	case "http":
		if !f.Insecure {
			return nil, ErrInsecureURL
		}
	default:
		return nil, ErrInsecureURL
	}
	if !hostAllowed(u.Hostname(), f.Hosts) {
		return nil, fmt.Errorf("%w: %s", ErrHostNotAllowed, u.Hostname())
	}
	return u, nil
}

// hostAllowed matches a host against the list, a host standing for itself and
// its subdomains. Suffix matching is anchored on a dot, because "notfreemdict
// .com" ends with "freemdict.com" as a string and is a different site.
func hostAllowed(host string, hosts []string) bool {
	if len(hosts) == 0 {
		return true
	}
	host = strings.ToLower(strings.Trim(host, "."))
	for _, h := range hosts {
		if host == h || strings.HasSuffix(host, "."+h) {
			return true
		}
	}
	return false
}

// partMeta is what makes resuming safe. A .part file's LENGTH says where to
// continue from; it says nothing about whether the thing being continued is
// still the same file. Splicing the tail of a re-uploaded archive onto the
// head of the old one produces a file that is corrupt in a way no checksum in
// this program would catch, so the validator the server gave us is written
// down beside the bytes and sent back as If-Range.
type partMeta struct {
	URL      string `json:"url"`
	ETag     string `json:"etag,omitempty"`
	Modified string `json:"modified,omitempty"`
	Total    int64  `json:"total,omitempty"`
}

// doneMeta records a COMPLETED download, beside the file, so the next import
// of the same link does not fetch it again (D134).
//
// It carries the validator rather than a checksum of the bytes for the reason
// the rest of this program gives about staleness (store.SourceChanged): the
// server already knows whether the file changed, and asking it costs one
// conditional request and no bytes, where hashing two gigabytes on a phone
// costs minutes and answers a question the server answered for free.
type doneMeta struct {
	URL      string `json:"url"`
	ETag     string `json:"etag,omitempty"`
	Modified string `json:"modified,omitempty"`
	// Size and File describe the result. Both are checked before the file is
	// offered back: a sidecar naming something that has been deleted, renamed
	// or truncated is not evidence of anything.
	Size int64  `json:"size"`
	File string `json:"file"`
}

// readDone reports a previous complete download of raw that is still on disk
// and still the size it was. The name is taken as a BASE NAME only, so a
// tampered sidecar cannot point the reuse path at a file elsewhere on the
// disk.
func readDone(sidecar, dir, raw string) (*doneMeta, string) {
	b, err := os.ReadFile(sidecar)
	if err != nil {
		return nil, ""
	}
	var m doneMeta
	if json.Unmarshal(b, &m) != nil || m.URL != raw || m.File == "" {
		return nil, ""
	}
	name := filepath.Base(m.File)
	if name != m.File || !Installable(name) {
		return nil, ""
	}
	full := filepath.Join(dir, name)
	fi, err := os.Stat(full)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() != m.Size || m.Size <= 0 {
		return nil, ""
	}
	return &m, full
}

// Progress is how a download reports itself while it runs. The NAME is part of
// the report and not an afterthought: the file a link turns out to name is
// decided by the server (a redirect, a Content-Disposition), it is the one
// thing a person watching a progress line actually wants to read, and until
// this carried it the only honest thing a caller could say was the host.
type Progress func(name string, done, total int64)

// Fetch downloads raw into dest's Downloads folder and describes the result as
// a Source. progress is called as bytes land, with the name the file will be
// saved under and the total when the server declared one, 0 when it did not.
//
// A failure or a cancel leaves the .part file in place on purpose: it is the
// resume point, and the next attempt continues from it.
func (f Fetcher) Fetch(ctx context.Context, dest, raw string, progress Progress) (Source, error) {
	u, err := f.Check(raw)
	if err != nil {
		return Source{}, err
	}
	dir := DownloadDir(dest)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Source{}, err
	}
	sweepPartials(dir)

	// The part name comes from the URL, and the FINAL name may come from a
	// Content-Disposition header we have not seen yet - so resume identity is
	// keyed on the URL in the sidecar, never on the file name.
	base := safeDirName(nameFromURL(u))
	part := filepath.Join(dir, base+".part")
	meta := part + ".meta"
	sidecar := filepath.Join(dir, base+".done")

	// Reuse beats resume, and is checked first. A completed download of this
	// URL means any .part beside it belongs to an ATTEMPT AT SOMETHING ELSE -
	// an older revision of the file, most likely - and appending to it would
	// splice two versions together.
	reuse, reusePath := readDone(sidecar, dir, u.String())
	var offset int64
	var prev partMeta
	if reuse == nil {
		offset, prev = resumePoint(part, meta, u.String())
	}
	resp, offset, err := f.get(ctx, u, offset, prev, reuse)
	if errors.Is(err, errNotModified) {
		// Nothing was transferred and nothing is temporary: this is the file
		// the user downloaded before, handed back under its own name.
		if progress != nil {
			progress(filepath.Base(reusePath), reuse.Size, reuse.Size)
		}
		return Source{Path: reusePath, Name: filepath.Base(reusePath)}, nil
	}
	if err != nil {
		return Source{}, err
	}
	defer resp.Body.Close()

	name, err := archiveName(u, resp)
	if err != nil {
		return Source{}, err
	}

	total := offset + resp.ContentLength // ContentLength is -1 when unknown
	if resp.ContentLength < 0 {
		total = 0
	}
	if total > maxDownloadBytes {
		return Source{}, errors.New("that file is too large to download")
	}
	writeMeta(meta, partMeta{
		URL: u.String(), ETag: resp.Header.Get("ETag"),
		Modified: resp.Header.Get("Last-Modified"), Total: total,
	})

	flags := os.O_CREATE | os.O_WRONLY
	if offset > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	fh, err := os.OpenFile(part, flags, 0o644)
	if err != nil {
		return Source{}, err
	}
	// From here the name is settled, so every tick carries it - including the
	// first, which copyBody sends before it reads a byte.
	var report func(done, total int64)
	if progress != nil {
		report = func(done, total int64) { progress(name, done, total) }
	}
	n, err := copyBody(ctx, fh, resp.Body, offset, total, report)
	if cerr := fh.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return Source{}, err
	}

	// Only now is the file whole, and only now does it get a name that is not
	// ".part" - which is the whole convention: nothing but a complete download
	// is ever visible under a name a scan or a user would take seriously.
	final, err := uniqueDir(dir, name) // a file, but the same "does not exist yet" question
	if err != nil {
		return Source{}, err
	}
	if err := os.Rename(part, final); err != nil {
		return Source{}, err
	}
	_ = os.Remove(meta)
	writeJSON(sidecar, doneMeta{
		URL: u.String(), ETag: resp.Header.Get("ETag"),
		Modified: resp.Header.Get("Last-Modified"),
		Size:     n, File: filepath.Base(final),
	})
	return Source{Path: final, Name: filepath.Base(final)}, nil
}

// get issues the request, retrying once from zero if the range was refused.
func (f Fetcher) get(ctx context.Context, u *url.URL, offset int64, prev partMeta, reuse *doneMeta) (*http.Response, int64, error) {
	resp, err := f.do(ctx, u, offset, prev, reuse)
	if err != nil {
		return nil, 0, err
	}
	if reuse != nil && resp.StatusCode == http.StatusNotModified {
		resp.Body.Close()
		return nil, 0, errNotModified
	}
	switch {
	case offset > 0 && resp.StatusCode == http.StatusOK:
		// The server ignored the range, or the validator no longer matches:
		// either way what is arriving is the whole file, so the old bytes are
		// not a head to append to.
		return resp, 0, nil
	case offset > 0 && resp.StatusCode == http.StatusRequestedRangeNotSatisfiable:
		// The .part is at least as long as the resource - a truncated write
		// that overshot, or a different file under the same URL. Start again
		// rather than guess which.
		resp.Body.Close()
		resp, err = f.do(ctx, u, 0, partMeta{}, reuse)
		if err != nil {
			return nil, 0, err
		}
		offset = 0
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		resp.Body.Close()
		return nil, 0, fmt.Errorf("the site answered %s", strings.ToLower(http.StatusText(resp.StatusCode)))
	}
	if resp.StatusCode == http.StatusOK {
		offset = 0
	}
	// A server that offers no validator at all cannot be asked a conditional
	// question, so the body is already on its way. Size is then the only
	// evidence there is, and it is the same evidence store.SourceChanged acts
	// on for a local file: equal length, same file, until something says
	// otherwise. Dropping the body here costs one connection and saves the
	// whole transfer.
	if reuse != nil && resp.StatusCode == http.StatusOK &&
		reuse.ETag == "" && reuse.Modified == "" &&
		resp.ContentLength == reuse.Size {
		resp.Body.Close()
		return nil, 0, errNotModified
	}
	return resp, offset, nil
}

func (f Fetcher) do(ctx context.Context, u *url.URL, offset int64, prev partMeta, reuse *doneMeta) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	// Identify honestly. Several of these sites answer a blank agent with a
	// challenge page, which would arrive here as "not an archive".
	req.Header.Set("User-Agent", "wudict")
	req.Header.Set("Accept", "*/*")
	if offset > 0 {
		req.Header.Set("Range", "bytes="+strconv.FormatInt(offset, 10)+"-")
		if v := prev.ETag; v != "" {
			req.Header.Set("If-Range", v)
		} else if v := prev.Modified; v != "" {
			req.Header.Set("If-Range", v)
		}
	}
	if reuse != nil {
		// Ask whether the file we already have is still current. A 304 ends
		// the download before it starts; anything else is a new revision and
		// is fetched whole, under its own name.
		if reuse.ETag != "" {
			req.Header.Set("If-None-Match", reuse.ETag)
		}
		if reuse.Modified != "" {
			req.Header.Set("If-Modified-Since", reuse.Modified)
		}
	}
	return f.client().Do(req)
}

// client builds the one http.Client this package uses. Two policies are wired
// into it rather than checked afterwards, because "afterwards" is too late for
// both: a redirect is re-validated before it is followed, and the address a
// name resolves to is judged at connect time, which is the only place a DNS
// answer that points somewhere else on the second lookup cannot slip past.
func (f Fetcher) client() *http.Client {
	d := &net.Dialer{Timeout: 20 * time.Second, KeepAlive: 30 * time.Second}
	if !f.Insecure {
		d.Control = func(network, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip := net.ParseIP(host)
			if ip == nil || local(ip) {
				// This server may be listening on a LAN, where the caller is
				// somebody else's browser. Without this, "download that for
				// me" is a way to reach hosts only this machine can see.
				return errors.New("that address is on the local network")
			}
			return nil
		}
	}
	return &http.Client{
		Transport: &http.Transport{
			DialContext:           d.DialContext,
			TLSHandshakeTimeout:   20 * time.Second,
			ResponseHeaderTimeout: 60 * time.Second,
			ForceAttemptHTTP2:     true,
			Proxy:                 http.ProxyFromEnvironment,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return errors.New("that link redirects too many times")
			}
			_, err := f.Check(req.URL.String())
			return err
		},
		// No overall timeout: a legitimate download is measured in minutes on
		// a phone. The context carries the cancel, and the per-stage timeouts
		// above are what catch a server that has stopped answering.
	}
}

// local reports an address this server should not be talked into reaching on
// somebody else's behalf.
func local(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() {
		return true
	}
	// Carrier-grade NAT (100.64.0.0/10): not "private" by IsPrivate, and
	// routinely where a mobile network's own equipment lives.
	if v4 := ip.To4(); v4 != nil {
		return v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127
	}
	return false
}

// copyBody streams the response to disk a megabyte at a time - the same buffer
// size extraction uses, for the same reason - reporting progress as it goes and
// stopping the moment the context is cancelled.
func copyBody(ctx context.Context, w io.Writer, r io.Reader, offset, total int64, progress func(done, total int64)) (int64, error) {
	buf := make([]byte, copyBufBytes)
	done := offset
	last := time.Now()
	if progress != nil {
		progress(done, total)
	}
	for {
		if err := ctx.Err(); err != nil {
			return done, err
		}
		n, err := r.Read(buf)
		if n > 0 {
			if done+int64(n) > maxDownloadBytes {
				return done, errors.New("that file is too large to download")
			}
			if _, werr := w.Write(buf[:n]); werr != nil {
				return done, werr
			}
			done += int64(n)
			// Throttled: a page polls this four times a second at most, and a
			// lock taken per megabyte is a lock taken thousands of times for
			// a number nobody reads.
			if progress != nil && time.Since(last) > 200*time.Millisecond {
				progress(done, total)
				last = time.Now()
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return done, err
		}
	}
	if progress != nil {
		progress(done, total)
	}
	return done, nil
}

// resumePoint is how far a previous attempt got, and the validator it recorded.
// Zero unless everything agrees: the sidecar exists, it names this URL, and it
// carries something the server can check the file against. A .part with no
// provenance is discarded rather than appended to.
func resumePoint(part, meta, raw string) (int64, partMeta) {
	fi, err := os.Stat(part)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() <= 0 {
		return 0, partMeta{}
	}
	b, err := os.ReadFile(meta)
	if err != nil {
		return 0, partMeta{}
	}
	var m partMeta
	if json.Unmarshal(b, &m) != nil || m.URL != raw {
		return 0, partMeta{}
	}
	if m.ETag == "" && m.Modified == "" {
		return 0, partMeta{}
	}
	return fi.Size(), m
}

func writeMeta(path string, m partMeta) { writeJSON(path, m) }

// writeJSON writes a sidecar. Best effort throughout: losing one costs a
// resume or a reuse, never the download.
func writeJSON(path string, m any) {
	b, err := json.Marshal(m)
	if err != nil {
		return
	}
	// Best effort: losing the sidecar costs a resume, not the download.
	_ = os.WriteFile(path, b, 0o644)
}

// sweepPartials removes abandoned downloads. Same three-layer thinking as the
// staging area: this is the layer for the attempt that was never retried, and
// the age cut is what keeps it from deleting the one the user is about to
// resume.
func sweepPartials(dir string) {
	cut := time.Now().Add(-partialStale)
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".done") {
			// A reuse record outlives its file whenever the user deletes the
			// download - which IMPORT_KEEP=delete does on every import - and a
			// record pointing at nothing would otherwise sit there forever.
			// No age cut applies: the file either exists or it does not.
			if !hasFileFor(dir, name) {
				_ = os.Remove(filepath.Join(dir, name))
			}
			continue
		}
		if !strings.HasSuffix(name, ".part") && !strings.HasSuffix(name, ".part.meta") {
			continue
		}
		fi, err := e.Info()
		if err != nil || fi.ModTime().After(cut) {
			continue
		}
		_ = os.Remove(filepath.Join(dir, name))
	}
}

// hasFileFor reports whether a .done sidecar still names a file that is on
// disk. Separate from readDone because that one also matches the URL, and a
// sidecar is worth keeping while its file exists whatever link produced it.
func hasFileFor(dir, sidecar string) bool {
	b, err := os.ReadFile(filepath.Join(dir, sidecar))
	if err != nil {
		return false
	}
	var m doneMeta
	if json.Unmarshal(b, &m) != nil || m.File == "" || filepath.Base(m.File) != m.File {
		return false
	}
	fi, err := os.Stat(filepath.Join(dir, m.File))
	return err == nil && fi.Mode().IsRegular()
}

// nameFromURL is the file name a link implies, before any header has been
// seen. Query strings are dropped: "?download=1" is not part of anybody's file
// name, and a download named after one is a file the user cannot recognise.
func nameFromURL(u *url.URL) string {
	name := path.Base(u.Path)
	if name == "." || name == "/" || name == "" {
		return "download"
	}
	if s, err := url.PathUnescape(name); err == nil {
		name = s
	}
	return name
}

// archiveName decides what the finished file is called, and refuses the
// download if the answer is neither an archive nor a dictionary file this
// build reads.
//
// Checked from the headers, before a byte of the body is taken: the common
// failure of a shared link is that it is a forum page or a login redirect, and
// the honest moment to say so is the first one, not after two gigabytes.
//
// A loose dictionary file is accepted on its NAME alone, because that is all
// there is: .mdx, .ifo, .dsl, .slob and .bgl have no registered media type and
// every server sends them as octet-stream. The one thing a name cannot survive
// is a server that contradicts it - an HTML page served as "oxford.mdx" is the
// login redirect wearing the file's name - so a declared text type is refused
// even when the name is perfect.
func archiveName(u *url.URL, resp *http.Response) (string, error) {
	ct, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	name := ""
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		if _, params, err := mime.ParseMediaType(cd); err == nil {
			// filename* (RFC 5987) is decoded by ParseMediaType and lands in
			// the same key, so there is nothing extra to do for it here.
			name = filepath.Base(strings.TrimSpace(params["filename"]))
		}
	}
	if !Installable(name) {
		name = nameFromURL(u)
	}
	if !Installable(name) {
		// A server that names nothing but declares the type is still
		// unambiguous; one that does neither is the login page.
		if ext := archiveExtForType(ct); ext != "" {
			name = strings.TrimSuffix(strings.TrimSuffix(name, "."), "/") + ext
		}
	}
	if !Installable(name) || isMarkup(ct) {
		return "", ErrNotArchive
	}
	return safeDirName(name), nil
}

// isMarkup reports the content types a dictionary is never served as and a
// challenge page always is. Deliberately only the two: a DSL dictionary IS
// text, and servers that send it as text/plain are sending the file.
func isMarkup(ct string) bool {
	switch strings.ToLower(ct) {
	case "text/html", "application/xhtml+xml":
		return true
	}
	return false
}

// archiveExtForType maps the content types an archive is served as onto the
// extension this package dispatches on. Only the types that ARE the formats
// read here: a generic octet-stream says nothing, and treating it as a zip
// would turn every unknown download into a failed extraction.
func archiveExtForType(ct string) string {
	switch strings.ToLower(ct) {
	case "application/zip", "application/x-zip-compressed", "application/x-zip":
		return ".zip"
	case "application/x-7z-compressed", "application/7z-compressed":
		return ".7z"
	}
	return ""
}
