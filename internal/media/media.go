package media

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/disintegration/imaging"
)

const (
	maxPhotoBytes     = 25 * 1024 * 1024
	thumbMaxDimension = 1200
)

// Store uploads and deletes media objects in S3. Enabled reports whether
// upload operations will work; callers should gate admin UI accordingly.
type Store struct {
	client  *s3.Client
	bucket  string
	cdnBase string // no trailing slash
	enabled bool
}

type Uploaded struct {
	Key       string
	ThumbKey  string
	Width     int
	Height    int
	MIMEType  string
	SizeBytes int64
}

func New(ctx context.Context, bucket, cdnBase string) *Store {
	s := &Store{bucket: bucket, cdnBase: strings.TrimRight(cdnBase, "/")}
	if bucket == "" {
		return s
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return s
	}
	if _, err := cfg.Credentials.Retrieve(ctx); err != nil {
		return s
	}
	s.client = s3.NewFromConfig(cfg)
	s.enabled = true
	return s
}

func (s *Store) Enabled() bool   { return s.enabled }
func (s *Store) CDNBase() string { return s.cdnBase }

// URL returns the public CDN URL for a stored key.
func (s *Store) URL(key string) string {
	if key == "" {
		return ""
	}
	return s.cdnBase + "/" + key
}

// UploadPhoto decodes the image, stores the original + a downscaled variant,
// and returns keys + dimensions.
func (s *Store) UploadPhoto(ctx context.Context, r io.Reader, filename, mimeType string) (*Uploaded, error) {
	if !s.enabled {
		return nil, errors.New("media store not configured")
	}

	buf, err := io.ReadAll(io.LimitReader(r, maxPhotoBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	if int64(len(buf)) > maxPhotoBytes {
		return nil, fmt.Errorf("file too large (max %dMB)", maxPhotoBytes/1024/1024)
	}

	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	ext, ok := photoExtFor(mimeType)
	if !ok {
		return nil, fmt.Errorf("unsupported image type: %s", mimeType)
	}

	img, _, err := image.Decode(bytes.NewReader(buf))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}

	slug, err := randomSlug()
	if err != nil {
		return nil, err
	}
	origKey := fmt.Sprintf("gallery/%s%s", slug, ext)
	thumbKey := fmt.Sprintf("gallery/%s-thumb.jpg", slug)

	// Upload original.
	if err := s.put(ctx, origKey, bytes.NewReader(buf), mimeType); err != nil {
		return nil, fmt.Errorf("upload original: %w", err)
	}

	// Generate + upload thumbnail (resize only if larger).
	thumb := img
	if img.Bounds().Dx() > thumbMaxDimension || img.Bounds().Dy() > thumbMaxDimension {
		thumb = imaging.Fit(img, thumbMaxDimension, thumbMaxDimension, imaging.Lanczos)
	}
	var tbuf bytes.Buffer
	if err := jpeg.Encode(&tbuf, thumb, &jpeg.Options{Quality: 85}); err != nil {
		return nil, fmt.Errorf("encode thumb: %w", err)
	}
	if err := s.put(ctx, thumbKey, bytes.NewReader(tbuf.Bytes()), "image/jpeg"); err != nil {
		// Best-effort cleanup of the original if thumb fails.
		_ = s.Delete(ctx, origKey)
		return nil, fmt.Errorf("upload thumb: %w", err)
	}

	return &Uploaded{
		Key:       origKey,
		ThumbKey:  thumbKey,
		Width:     img.Bounds().Dx(),
		Height:    img.Bounds().Dy(),
		MIMEType:  mimeType,
		SizeBytes: int64(len(buf)),
	}, nil
}

// Delete removes an object. Missing-key is not an error.
func (s *Store) Delete(ctx context.Context, key string) error {
	if !s.enabled || key == "" {
		return nil
	}
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	return err
}

func (s *Store) put(ctx context.Context, key string, body io.Reader, contentType string) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:       aws.String(s.bucket),
		Key:          aws.String(key),
		Body:         body,
		ContentType:  aws.String(contentType),
		CacheControl: aws.String("public, max-age=31536000, immutable"),
	})
	return err
}

// image/jpeg + image/png decoders are already registered via the imports above.
var _ = png.Decode

func photoExtFor(mime string) (string, bool) {
	switch mime {
	case "image/jpeg", "image/jpg":
		return ".jpg", true
	case "image/png":
		return ".png", true
	case "image/webp":
		return ".webp", true
	default:
		return "", false
	}
}

func randomSlug() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
