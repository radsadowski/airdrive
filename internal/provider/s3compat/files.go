// StorageProvider methods for the s3compat bucket provider.
package s3compat

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"

	"github.com/radosny/airdrive/internal/provider"
)

// List returns the full directory — minio-go paginates internally, so the
// cursor is always empty (the index is the UI's read path).
func (p *Provider) List(ctx context.Context, locationID, dirID string, sort provider.SortSpec, cursor string, limit int) (provider.ListPage, error) {
	var items []provider.FileInfo
	for obj := range p.client.ListObjects(ctx, p.bucket, minio.ListObjectsOptions{Prefix: dirID, Recursive: false}) {
		if obj.Err != nil {
			return provider.ListPage{}, fmt.Errorf("s3compat: list %q: %w", dirID, obj.Err)
		}
		if obj.Key == dirID {
			continue
		}
		items = append(items, p.toFileInfo(obj.Key, obj.Size, obj.ETag, obj.LastModified))
	}
	return provider.ListPage{Items: items}, nil
}

// Stat HEADs the object; dir keys without a marker object (implicit dirs)
// are synthesized with zero metadata.
func (p *Provider) Stat(ctx context.Context, locationID, fileID string) (provider.FileInfo, error) {
	obj, err := p.client.StatObject(ctx, p.bucket, fileID, minio.StatObjectOptions{})
	if err != nil {
		if isDirKey(fileID) {
			return p.toFileInfo(fileID, 0, "", time.Time{}), nil
		}
		return provider.FileInfo{}, fmt.Errorf("s3compat: stat %q: %w", fileID, err)
	}
	return p.toFileInfo(obj.Key, obj.Size, obj.ETag, obj.LastModified), nil
}

// Mkdir creates the zero-byte marker object that represents the directory.
func (p *Provider) Mkdir(ctx context.Context, locationID, parentID, name string) (provider.FileInfo, error) {
	key := joinKey(parentID, name) + "/"
	if _, err := p.client.PutObject(ctx, p.bucket, key, bytes.NewReader(nil), 0, minio.PutObjectOptions{}); err != nil {
		return provider.FileInfo{}, fmt.Errorf("s3compat: mkdir %q: %w", key, err)
	}
	return p.toFileInfo(key, 0, "", time.Now().UTC()), nil
}

func (p *Provider) Rename(ctx context.Context, locationID, fileID, newName string) (provider.FileInfo, error) {
	if isDirKey(fileID) {
		newKey := joinKey(parentOf(fileID), newName) + "/"
		if err := p.movePrefix(ctx, fileID, newKey); err != nil {
			return provider.FileInfo{}, err
		}
		return p.toFileInfo(newKey, 0, "", time.Time{}), nil
	}
	newKey := joinKey(parentOf(fileID), newName)
	if err := p.copyObject(ctx, newKey, fileID); err != nil {
		return provider.FileInfo{}, err
	}
	if err := p.remove(ctx, fileID); err != nil {
		return provider.FileInfo{}, err
	}
	return p.toFileInfo(newKey, 0, "", time.Now().UTC()), nil
}

// MoveWithin moves files and directory trees under destDirID — a server-side
// copy per object, then removal of the sources.
func (p *Provider) MoveWithin(ctx context.Context, locationID string, fileIDs []string, destDirID string) error {
	for _, id := range fileIDs {
		if isDirKey(id) {
			if err := p.movePrefix(ctx, id, joinKey(destDirID, baseName(id)+"/")); err != nil {
				return err
			}
			continue
		}
		dst := joinKey(destDirID, baseName(id))
		if err := p.copyObject(ctx, dst, id); err != nil {
			return err
		}
		if err := p.remove(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

// Delete moves objects to the in-bucket trash prefix — never permanent.
func (p *Provider) Delete(ctx context.Context, locationID string, fileIDs []string) error {
	for _, id := range fileIDs {
		ts := fmt.Sprintf("%d", time.Now().UnixNano())
		if isDirKey(id) {
			if err := p.movePrefix(ctx, id, TrashPrefix+ts+"/"+id); err != nil {
				return err
			}
			continue
		}
		if err := p.copyObject(ctx, TrashPrefix+ts+"/"+id, id); err != nil {
			return err
		}
		if err := p.remove(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

func (p *Provider) Download(ctx context.Context, locationID, fileID string) (io.ReadCloser, provider.FileInfo, error) {
	obj, err := p.client.GetObject(ctx, p.bucket, fileID, minio.GetObjectOptions{})
	if err != nil {
		return nil, provider.FileInfo{}, fmt.Errorf("s3compat: get %q: %w", fileID, err)
	}
	stat, err := obj.Stat()
	if err != nil {
		obj.Close()
		return nil, provider.FileInfo{}, fmt.Errorf("s3compat: stat %q: %w", fileID, err)
	}
	return obj, p.toFileInfo(stat.Key, stat.Size, stat.ETag, stat.LastModified), nil
}

func (p *Provider) Upload(ctx context.Context, locationID, parentID, name string, r io.Reader, size int64) (provider.FileInfo, error) {
	key := joinKey(parentID, name)
	mime := provider.MimeFor(name, false)
	if _, err := p.client.PutObject(ctx, p.bucket, key, r, size, minio.PutObjectOptions{ContentType: mime}); err != nil {
		return provider.FileInfo{}, fmt.Errorf("s3compat: upload %q: %w", key, err)
	}
	return p.toFileInfo(key, size, "", time.Now().UTC()), nil
}

func (p *Provider) copyObject(ctx context.Context, dst, src string) error {
	_, err := p.client.CopyObject(ctx,
		minio.CopyDestOptions{Bucket: p.bucket, Object: dst},
		minio.CopySrcOptions{Bucket: p.bucket, Object: src},
	)
	if err != nil {
		return fmt.Errorf("s3compat: copy %q → %q: %w", src, dst, err)
	}
	return nil
}

func (p *Provider) remove(ctx context.Context, key string) error {
	if err := p.client.RemoveObject(ctx, p.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("s3compat: remove %q: %w", key, err)
	}
	return nil
}

// movePrefix copies every object under oldPrefix to the equivalent key under
// newPrefix, then removes the sources — S3 has no atomic prefix rename.
func (p *Provider) movePrefix(ctx context.Context, oldPrefix, newPrefix string) error {
	var keys []string
	for obj := range p.client.ListObjects(ctx, p.bucket, minio.ListObjectsOptions{Prefix: oldPrefix, Recursive: true}) {
		if obj.Err != nil {
			return fmt.Errorf("s3compat: walk %q: %w", oldPrefix, obj.Err)
		}
		keys = append(keys, obj.Key)
	}
	for _, k := range keys {
		if err := p.copyObject(ctx, newPrefix+strings.TrimPrefix(k, oldPrefix), k); err != nil {
			return err
		}
	}
	for _, k := range keys {
		if err := p.remove(ctx, k); err != nil {
			return err
		}
	}
	return nil
}
