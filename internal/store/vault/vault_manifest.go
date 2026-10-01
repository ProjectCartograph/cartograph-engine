package vault

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/yamlfmt"
	"go.yaml.in/yaml/v3"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

// VaultManifest represents the vault.yaml file
type VaultManifest struct {
	APIVersion string        `yaml:"apiVersion"`
	Kind       string        `yaml:"kind"`
	Metadata   VaultMetadata `yaml:"metadata"`
	Spec       VaultSpec     `yaml:"spec"`
}

type VaultMetadata struct {
	ID string `yaml:"id"`
}

type VaultSpec struct {
	Include []string `yaml:"include"`
}

// readVaultManifest reads vault.yaml from the vault root.
// Returns an empty manifest with default values if the file doesn't exist.
func (m *ManifestStore) readVaultManifest(ctx context.Context) (VaultManifest, error) {
	vaultYAMLPath := filepath.Join(m.vaultDir, "vault.yaml")

	// Read if exists
	data, err := os.ReadFile(vaultYAMLPath)
	if err != nil && !os.IsNotExist(err) {
		return VaultManifest{}, fmt.Errorf("read vault.yaml: %w", err)
	}

	// If file doesn't exist, return a new vault with all present files
	if os.IsNotExist(err) {
		return m.generateVaultManifest(ctx)
	}

	// Parse existing vault.yaml
	var vault VaultManifest
	if err := yaml.Unmarshal(data, &vault); err != nil {
		return VaultManifest{}, fmt.Errorf("parse vault.yaml: %w", err)
	}

	// Ensure required fields
	if vault.APIVersion == "" {
		vault.APIVersion = "cartograph/v1"
	}
	if vault.Kind == "" {
		vault.Kind = "Vault"
	}
	if vault.Metadata.ID == "" {
		vault.Metadata.ID = filepath.Base(m.vaultDir)
	}
	if vault.Spec.Include == nil {
		vault.Spec.Include = []string{}
	}

	return vault, nil
}

// generateVaultManifest creates a new vault.yaml with all present files
func (m *ManifestStore) generateVaultManifest(ctx context.Context) (VaultManifest, error) {
	vault := VaultManifest{
		APIVersion: "cartograph/v1",
		Kind:       "Vault",
		Metadata:   VaultMetadata{ID: filepath.Base(m.vaultDir)},
		Spec:       VaultSpec{Include: []string{}},
	}

	// Scan directories and collect all <Kind>/<id> entries
	entries, err := os.ReadDir(m.vaultDir)
	if err != nil {
		return vault, fmt.Errorf("read vault dir: %w", err)
	}

	var refs []string
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == ".cartograph" {
			continue
		}
		kind := entry.Name()
		kindDir := filepath.Join(m.vaultDir, kind)
		kindEntries, err := os.ReadDir(kindDir)
		if err != nil {
			return vault, fmt.Errorf("read kind dir %s: %w", kind, err)
		}

		for _, fe := range kindEntries {
			if !fe.IsDir() && (fe.Name() != "vault.yaml") {
				if name := fe.Name(); len(name) > 5 && name[len(name)-5:] == m.ext() {
					id := name[:len(name)-5]
					refs = append(refs, kind+"/"+id)
				}
			}
		}
	}

	sort.Strings(refs)
	vault.Spec.Include = refs

	// Write vault.yaml and log it. An adapter never writes to stdout.
	if err := m.writeVaultManifest(vault); err != nil {
		return vault, err
	}
	slog.Debug("generated vault.yaml", "entries", len(refs))

	return vault, nil
}

// writeVaultManifest writes the vault manifest to vault.yaml
func (m *ManifestStore) writeVaultManifest(vault VaultManifest) error {
	data, err := yamlfmt.Marshal(vault)
	if err != nil {
		return fmt.Errorf("marshal vault.yaml: %w", err)
	}

	vaultYAMLPath := filepath.Join(m.vaultDir, "vault.yaml")

	// Write to temp file and rename
	tempPath := vaultYAMLPath + ".tmp"
	if err := os.WriteFile(tempPath, data, 0644); err != nil {
		return fmt.Errorf("write temp vault.yaml: %w", err)
	}

	if err := os.Rename(tempPath, vaultYAMLPath); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("rename vault.yaml: %w", err)
	}

	return nil
}

// getVaultYAMLBytes serializes the given VaultManifest to YAML bytes.
// Used to prepare vault.yaml content for inclusion in an ApplyUnit.
func (m *ManifestStore) getVaultYAMLBytes(vault VaultManifest) ([]byte, error) {
	data, err := yamlfmt.Marshal(vault)
	if err != nil {
		return nil, fmt.Errorf("marshal vault.yaml: %w", err)
	}
	return data, nil
}

// vaultHash returns the SHA-256 hash of vault.yaml
func (m *ManifestStore) vaultHash(ctx context.Context) (string, error) {
	vaultYAMLPath := filepath.Join(m.vaultDir, "vault.yaml")
	data, err := os.ReadFile(vaultYAMLPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read vault.yaml: %w", err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), nil
}

// recordVaultHash stores the vault.yaml hash in the index's meta facts.
func (m *ManifestStore) recordVaultHash(ctx context.Context, hash string) error {
	return m.index.PutMeta(ctx, "vault.yaml.hash", hash)
}

// rehydrate rebuilds the in-memory state from vault.yaml and the included manifests
func (m *ManifestStore) rehydrate(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.cache = make(map[string]*store.Version)
	m.hashes = make(map[string]string)

	// Read vault.yaml
	vault, err := m.readVaultManifest(ctx)
	if err != nil {
		return fmt.Errorf("read vault manifest: %w", err)
	}

	// Load only the included manifests
	for _, ref := range vault.Spec.Include {
		parts := strings.Split(ref, "/")
		if len(parts) != 2 {
			continue
		}
		kind, id := parts[0], parts[1]
		path := filepath.Join(m.vaultDir, kind, id+m.ext())

		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				// File doesn't exist, skip but don't error
				continue
			}
			return fmt.Errorf("read %s: %w", path, err)
		}

		hash := sha256String(data)
		key := kind + "/" + id

		// Load into cache as current content (no versioning)
		v := &store.Version{
			Kind:   kind,
			ID:     id,
			Number: 0,
			YAML:   data,
		}

		m.cache[key] = v
		m.hashes[key] = hash

		// Ensure file hash is recorded
		if err := m.recordFileHash(ctx, kind, id, hash, time.Now().Format(timeLayout)); err != nil {
			return err
		}
	}

	// Record the vault.yaml hash to detect stale index on next open
	hash, err := m.vaultHash(ctx)
	if err != nil {
		return err
	}
	if err := m.recordVaultHash(ctx, hash); err != nil {
		return err
	}

	return nil
}
