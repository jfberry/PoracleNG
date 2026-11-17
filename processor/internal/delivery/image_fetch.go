package delivery

import (
	"net/http"
	"time"

	"github.com/jellydator/ttlcache/v3"
	"golang.org/x/sync/singleflight"
)

// Embed images kept after download, so destinations that reach the same image
// a little later reuse it. Bounded by count: tiles are tens to a few hundred
// KB, so 64 entries stay within a few tens of MB.
const (
	imageCacheTTL  = 2 * time.Minute
	imageCacheSize = 64
)

// imageFetcher downloads embed images for upload and shares the work between
// destinations. One event fans out to many destinations carrying the same
// image (the tile, the fallback image when the tile failed, a pokemon icon),
// and each delivery lane used to download it separately: under load that was
// N simultaneous GETs to a tileserver that was already struggling. Concurrent
// fetches of one URL now make a single download, and a successful download is
// cached for imageCacheTTL. Failures are not cached.
type imageFetcher struct {
	client *http.Client
	group  singleflight.Group
	cache  *ttlcache.Cache[string, []byte]
}

func newImageFetcher(client *http.Client) *imageFetcher {
	return &imageFetcher{
		client: client,
		cache: ttlcache.New(
			ttlcache.WithTTL[string, []byte](imageCacheTTL),
			ttlcache.WithCapacity[string, []byte](imageCacheSize),
			ttlcache.WithDisableTouchOnHit[string, []byte](),
		),
	}
}

// fetch returns the image at url. The returned slice is shared between
// callers and must not be modified.
func (f *imageFetcher) fetch(url string) ([]byte, error) {
	if item := f.cache.Get(url); item != nil {
		return item.Value(), nil
	}
	v, err, _ := f.group.Do(url, func() (any, error) {
		// A download that finished between the Get above and this call has
		// already filled the cache.
		if item := f.cache.Get(url); item != nil {
			return item.Value(), nil
		}
		data, err := DownloadImage(f.client, url)
		if err != nil {
			return nil, err
		}
		f.cache.Set(url, data, ttlcache.DefaultTTL)
		return data, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]byte), nil
}
