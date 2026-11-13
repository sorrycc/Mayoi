package main

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
)

// markdownRemoteMax is the most of a picture of the web the preview
// fetches, and markdownFetches bounds how many it fetches at once.
const markdownRemoteMax = 10 << 20

var markdownFetches = make(chan struct{}, 4)

// markdownRemote keeps the pictures of the web the previews asked for this
// session, by their URLs, each fetched once, whether it showed or not.
var markdownRemote = struct {
	sync.Mutex
	images map[string]*markdownRemoteImage
	// get fetches a picture; the tests have it fail rather than go out.
	get func(url string) ([]byte, error)
}{images: map[string]*markdownRemoteImage{}, get: markdownHTTPGet}

type markdownRemoteImage struct {
	done    bool
	image   markdownInline
	arrived []func()
}

var markdownHTTP = &http.Client{Timeout: 10 * time.Second}

func markdownHTTPGet(url string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "image/*")
	resp, err := markdownHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New(resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, markdownRemoteMax+1))
	if err == nil && len(data) > markdownRemoteMax {
		err = errors.New("image too large")
	}
	return data, err
}

// markdownFetch is the picture at url once it is fetched. Until then, it
// fetches it in the background, if arrived is not nil, and calls arrived
// once it is fetched or failed.
func markdownFetch(url string, arrived func()) (markdownInline, bool) {
	m := &markdownRemote
	m.Lock()
	defer m.Unlock()
	if e, ok := m.images[url]; ok {
		if !e.done && arrived != nil {
			e.arrived = append(e.arrived, arrived)
		}
		return e.image, e.done
	}
	if arrived == nil {
		return markdownInline{}, false
	}
	e := &markdownRemoteImage{arrived: []func(){arrived}}
	m.images[url] = e
	get := m.get
	go func() {
		defer recoverCrash()
		markdownFetches <- struct{}{}
		data, err := get(url)
		<-markdownFetches
		var image markdownInline
		// The preview draws an SVG without its text, which would leave
		// a badge's words out: its alt text shows rather.
		if svg := bytes.Contains(data[:min(len(data), 1024)], []byte("<svg")); err == nil && !(svg && bytes.Contains(data, []byte("<text"))) {
			image = markdownDecode(data, svg)
		}
		m.Lock()
		e.done, e.image = true, image
		calls := e.arrived
		e.arrived = nil
		m.Unlock()
		// Failed, it is called too: Live Preview's card for it goes.
		for _, f := range calls {
			f()
		}
	}()
	return markdownInline{}, false
}

// markdownFetching reports whether the picture of the web at url is on
// its way.
func markdownFetching(url string) bool {
	m := &markdownRemote
	m.Lock()
	defer m.Unlock()
	e, ok := m.images[url]
	return ok && !e.done
}

// markdownDecode is a picture's bitmap and size, of an SVG or another
// image, without a bitmap if it is none.
func markdownDecode(data []byte, svg bool) markdownInline {
	var image markdownInline
	if svg {
		if s, err := ui.ParseSVG(data); err == nil {
			image.bitmap = s
			image.w, image.h = s.Size()
			if image.w <= 0 || image.h <= 0 {
				image.bitmap = nil
			}
		}
		return image
	}
	if bm, w, h, err := decodeImageData(data); err == nil {
		image.bitmap, image.w, image.h = bm, float32(w), float32(h)
	}
	return image
}
