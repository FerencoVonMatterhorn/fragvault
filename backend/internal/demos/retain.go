package demos

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blockblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
)

// retainTimeout bounds an upload the same way downloadTimeout bounds a
// download: generous, because these are hundreds of MB, but finite, because a
// stuck upload would hold the single analysis worker.
const retainTimeout = 10 * time.Minute

// retainBlockSize is what the file is cut into for upload. Large enough that a
// 300 MB demo is a few dozen requests rather than hundreds, small enough that
// the buffers don't matter on a VM sized for a web server.
const retainBlockSize = 8 << 20 // 8 MiB

// BlobDemoStore keeps parsed demos in Azure Blob Storage.
//
// Authenticated with a container-scoped SAS URL rather than a managed
// identity, matching how every other credential on the box works: a value in
// /opt/fragvault/.env. A SAS also means this code has no notion of Entra, no
// token cache and nothing to do on startup. The cost is an expiry date — see
// the README for regenerating it.
type BlobDemoStore struct {
	container *container.Client
}

// NewBlobDemoStore builds a store from a container SAS URL, of the form
// https://<account>.blob.core.windows.net/demos?sv=...&sig=...
func NewBlobDemoStore(sasURL string) (*BlobDemoStore, error) {
	c, err := container.NewClientWithNoCredential(sasURL, nil)
	if err != nil {
		return nil, fmt.Errorf("building demo container client: %w", err)
	}
	return &BlobDemoStore{container: c}, nil
}

// Retain uploads a demo and returns the blob name it was stored under.
//
// The file is stored exactly as it was downloaded, which means it may still be
// bzip2-compressed, and the name says .dem either way. That is deliberate:
// ParseFile already sniffs the magic bytes rather than trusting the extension,
// because the URL a demo arrives from may be redirected or simply named badly,
// and anything reading these blobs should do the same.
func (s *BlobDemoStore) Retain(ctx context.Context, shareCode, path string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, retainTimeout)
	defer cancel()

	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("opening demo for upload: %w", err)
	}
	defer f.Close()

	// The share code is already the primary key of a match, so it is the
	// natural name here, and re-analysing a match overwrites its own blob
	// rather than accumulating copies.
	name := shareCode + ".dem"

	if _, err := s.container.NewBlockBlobClient(name).UploadFile(ctx, f, &blockblob.UploadFileOptions{
		BlockSize:   retainBlockSize,
		Concurrency: 2,
	}); err != nil {
		return "", fmt.Errorf("uploading demo: %w", err)
	}

	return name, nil
}
