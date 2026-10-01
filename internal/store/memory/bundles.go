package memory

import (
	"context"
	"fmt"
	"sync"

	"github.com/ProjectCartograph/cartograph-engine/internal/store"
)

// BundleStore keeps handoff bundles in memory: what a test needs to prove
// a handoff recorded what it rendered, without a directory.
type BundleStore struct {
	mu      sync.Mutex
	bundles map[string]map[string][]byte // location -> name -> content
}

var _ store.BundleStore = (*BundleStore)(nil)

// NewBundleStore returns an empty BundleStore.
func NewBundleStore() *BundleStore {
	return &BundleStore{bundles: map[string]map[string][]byte{}}
}

// PutBundle stores files under "handoff/<project>/v<n>".
func (b *BundleStore) PutBundle(_ context.Context, projectID string, version int, files map[string][]byte) (store.Bundle, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	location := fmt.Sprintf("handoff/%s/v%d", projectID, version)
	copied := make(map[string][]byte, len(files))
	paths := make(map[string]string, len(files))
	for name, content := range files {
		copied[name] = append([]byte(nil), content...)
		paths[name] = location + "/" + name
	}
	b.bundles[location] = copied
	return store.Bundle{Location: location, Paths: paths}, nil
}

// Get returns one stored file, for tests.
func (b *BundleStore) Get(location, name string) ([]byte, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	files, ok := b.bundles[location]
	if !ok {
		return nil, false
	}
	content, ok := files[name]
	return content, ok
}
