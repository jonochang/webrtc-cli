package dtls

import "sync"

type handshakeCacheItem struct {
	typ             handshakeType
	isClient        bool
	messageSequence uint16
	data            []byte
}

type handshakeCachePullRule struct {
	typ      handshakeType
	isClient bool
}

type handshakeCache struct {
	cache []*handshakeCacheItem
	mu    sync.Mutex
}

func newHandshakeCache() *handshakeCache {
	return &handshakeCache{}
}

func (h *handshakeCache) push(data []byte, messageSequence uint16, typ handshakeType, isClient bool) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	for _, i := range h.cache {
		if i.messageSequence == messageSequence &&
			i.isClient == isClient {
			return false
		}
	}

	h.cache = append(h.cache, &handshakeCacheItem{
		data:            append([]byte{}, data...),
		messageSequence: messageSequence,
		typ:             typ,
		isClient:        isClient,
	})
	return true
}

// returns a list handshakes that match the requested rules
// the list will contain null entries for rules that can't be satisfied
// multiple entries may match a rule, but only the last match is returned (ie ClientHello with cookies)
func (h *handshakeCache) pull(rules ...handshakeCachePullRule) []*handshakeCacheItem {
	h.mu.Lock()
	defer h.mu.Unlock()

	out := make([]*handshakeCacheItem, len(rules))
	for i, r := range rules {
		for _, c := range h.cache {
			if c.typ == r.typ && c.isClient == r.isClient {
				switch {
				case out[i] == nil:
					out[i] = c
				case out[i].messageSequence < c.messageSequence:
					out[i] = c
				}
			}
		}
	}

	return out
}

// pullAndMerge calls pull and then merges the results, ignoring any null entries
func (h *handshakeCache) pullAndMerge(rules ...handshakeCachePullRule) []byte {
	merged := []byte{}

	for _, p := range h.pull(rules...) {
		if p != nil {
			merged = append(merged, p.data...)
		}
	}
	return merged
}

// sessionHash returns the session hash for Extended Master Secret support
// https://tools.ietf.org/html/draft-ietf-tls-session-hash-06#section-4
func (h *handshakeCache) sessionHash(hf hashFunc) ([]byte, error) {
	merged := []byte{}

	// Order defined by https://tools.ietf.org/html/rfc5246#section-7.3
	handshakeBuffer := h.pull(
		handshakeCachePullRule{handshakeTypeClientHello, true},
		handshakeCachePullRule{handshakeTypeServerHello, false},
		handshakeCachePullRule{handshakeTypeCertificate, false},
		handshakeCachePullRule{handshakeTypeServerKeyExchange, false},
		handshakeCachePullRule{handshakeTypeCertificateRequest, false},
		handshakeCachePullRule{handshakeTypeServerHelloDone, false},
		handshakeCachePullRule{handshakeTypeCertificate, true},
		handshakeCachePullRule{handshakeTypeClientKeyExchange, true},
	)

	for _, p := range handshakeBuffer {
		if p == nil {
			continue
		}

		merged = append(merged, p.data...)
	}

	hash := hf()
	if _, err := hash.Write(merged); err != nil {
		return []byte{}, err
	}

	return hash.Sum(nil), nil
}
