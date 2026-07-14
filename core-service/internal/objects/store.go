// Package objects wraps the S3 bucket core keeps its own binaries in: mirrored avatars and
// photos attached to comments.
package objects

import (
	"bytes"
	"context"
	"io"
	"net/http"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Store struct {
	client *minio.Client
	bucket string
}

type StoreConfig struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	UseSSL    bool
}

func NewStore(ctx context.Context, cfg StoreConfig) (*Store, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, err
	}
	s := &Store{client: client, bucket: cfg.Bucket}
	return s, s.ensureBucket(ctx)
}

// The bucket stays private: bytes are served through core, which checks the bearer token.
func (s *Store) ensureBucket(ctx context.Context) error {
	ok, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	return s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{})
}

func (s *Store) Put(ctx context.Context, key string, data []byte) error {
	contentType := http.DetectContentType(data)
	_, err := s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: contentType})
	return err
}

// Open streams an object back. The caller closes the reader.
func (s *Store) Open(ctx context.Context, key string) (io.ReadCloser, string, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, "", err
	}
	// GetObject is lazy: the first Stat is what actually reports a missing object
	info, err := obj.Stat()
	if err != nil {
		obj.Close()
		return nil, "", err
	}
	return obj, info.ContentType, nil
}

func (s *Store) Delete(ctx context.Context, key string) error {
	return s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
}
