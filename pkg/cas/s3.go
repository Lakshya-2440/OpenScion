package cas

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// RemoteStorage defines the interface for distributed object storage backends
type RemoteStorage interface {
	PutChunk(hash string, data []byte) error
	GetChunk(hash string) ([]byte, error)
	PutManifest(blobHash string, data []byte) error
	GetManifest(blobHash string) ([]byte, error)
}

// S3Config holds connection credentials for AWS S3, Cloudflare R2, or MinIO
type S3Config struct {
	Endpoint        string `json:"endpoint"`
	Bucket          string `json:"bucket"`
	Region          string `json:"region"`
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	UseSSL          bool   `json:"use_ssl"`
}

// S3Store implements RemoteStorage on top of any S3-compatible cloud blob store
type S3Store struct {
	client *minio.Client
	bucket string
}

// NewS3Store initializes connection to S3/MinIO and ensures the target bucket exists
func NewS3Store(cfg S3Config) (*S3Store, error) {
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("S3 bucket name required")
	}

	endpoint := cfg.Endpoint
	useSSL := cfg.UseSSL

	// Clean protocol if provided in endpoint
	if strings.HasPrefix(endpoint, "http://") {
		endpoint = strings.TrimPrefix(endpoint, "http://")
		useSSL = false
	} else if strings.HasPrefix(endpoint, "https://") {
		endpoint = strings.TrimPrefix(endpoint, "https://")
		useSSL = true
	}

	if endpoint == "" {
		endpoint = "s3.amazonaws.com"
		useSSL = true
	}

	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}

	minioClient, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure: useSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize S3 client: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Verify or create bucket
	exists, err := minioClient.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		// Log error but allow operation in case permissions restrict BucketExists
		fmt.Printf("\033[33mWarning: S3 bucket check on %s: %v\033[0m\n", cfg.Bucket, err)
	} else if !exists {
		err = minioClient.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{Region: cfg.Region})
		if err != nil {
			return nil, fmt.Errorf("failed to create bucket %s: %w", cfg.Bucket, err)
		}
	}

	return &S3Store{
		client: minioClient,
		bucket: cfg.Bucket,
	}, nil
}

// NewS3StoreFromEnv auto-discovers S3 configuration from environment variables
func NewS3StoreFromEnv() (*S3Store, error) {
	bucket := os.Getenv("S3_BUCKET")
	if bucket == "" {
		return nil, nil // No S3 configured
	}

	endpoint := os.Getenv("S3_ENDPOINT")
	accessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	if accessKey == "" {
		accessKey = os.Getenv("S3_ACCESS_KEY")
	}
	secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	if secretKey == "" {
		secretKey = os.Getenv("S3_SECRET_KEY")
	}
	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = os.Getenv("S3_REGION")
		if region == "" {
			region = "us-east-1"
		}
	}

	useSSL := true
	if strings.ToLower(os.Getenv("S3_USE_SSL")) == "false" || strings.HasPrefix(endpoint, "http://") {
		useSSL = false
	}

	return NewS3Store(S3Config{
		Endpoint:        endpoint,
		Bucket:          bucket,
		Region:          region,
		AccessKeyID:     accessKey,
		SecretAccessKey: secretKey,
		UseSSL:          useSSL,
	})
}

// ParseS3URL parses a URL like s3://bucket/endpoint or standard params
func ParseS3URL(s3URL string) (*S3Config, error) {
	u, err := url.Parse(s3URL)
	if err != nil {
		return nil, err
	}

	bucket := u.Host
	endpoint := u.Query().Get("endpoint")
	region := u.Query().Get("region")
	user := u.User.Username()
	pass, _ := u.User.Password()

	return &S3Config{
		Endpoint:        endpoint,
		Bucket:          bucket,
		Region:          region,
		AccessKeyID:     user,
		SecretAccessKey: pass,
		UseSSL:          !strings.HasPrefix(endpoint, "http://"),
	}, nil
}

func s3ChunkKey(hash string) string {
	if len(hash) < 4 {
		return "chunks/" + hash + ".chunk"
	}
	return fmt.Sprintf("chunks/%s/%s/%s.chunk", hash[0:2], hash[2:4], hash)
}

func s3ManifestKey(hash string) string {
	if len(hash) < 4 {
		return "manifests/" + hash + ".json"
	}
	return fmt.Sprintf("manifests/%s/%s/%s.json", hash[0:2], hash[2:4], hash)
}

func (s *S3Store) PutChunk(hash string, data []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	key := s3ChunkKey(hash)
	reader := bytes.NewReader(data)
	_, err := s.client.PutObject(ctx, s.bucket, key, reader, int64(len(data)), minio.PutObjectOptions{
		ContentType: "application/octet-stream",
	})
	return err
}

func (s *S3Store) GetChunk(hash string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	key := s3ChunkKey(hash)
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer obj.Close()

	return io.ReadAll(obj)
}

func (s *S3Store) PutManifest(blobHash string, data []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	key := s3ManifestKey(blobHash)
	reader := bytes.NewReader(data)
	_, err := s.client.PutObject(ctx, s.bucket, key, reader, int64(len(data)), minio.PutObjectOptions{
		ContentType: "application/json",
	})
	return err
}

func (s *S3Store) GetManifest(blobHash string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	key := s3ManifestKey(blobHash)
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer obj.Close()

	return io.ReadAll(obj)
}
