package s3upload

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type fakeS3Client struct {
	input *s3.PutObjectInput
	body  []byte
	err   error
}

func (c *fakeS3Client) PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	c.input = params
	body, err := io.ReadAll(params.Body)
	if err != nil {
		return nil, err
	}
	c.body = body
	if c.err != nil {
		return nil, c.err
	}
	return &s3.PutObjectOutput{}, nil
}

func TestWriterProviderUploadsOnClose(t *testing.T) {
	client := &fakeS3Client{}
	wp := NewWriterProvider(context.Background(), client, "bucket", "tiles",
		WithContentType(func(filename string) string {
			if filename == "tiles/data/d.glb" {
				return "model/gltf-binary"
			}
			return ""
		}),
	)

	w, err := wp("data/d.glb")
	if err != nil {
		t.Fatalf("writer provider: %v", err)
	}
	if _, err := w.Write([]byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close/upload: %v", err)
	}

	if client.input == nil {
		t.Fatal("expected PutObject to be called")
	}
	if *client.input.Bucket != "bucket" {
		t.Fatalf("unexpected bucket %q", *client.input.Bucket)
	}
	if *client.input.Key != "tiles/data/d.glb" {
		t.Fatalf("unexpected key %q", *client.input.Key)
	}
	if string(client.body) != "hello" {
		t.Fatalf("unexpected body %q", string(client.body))
	}
	if client.input.ContentType == nil || *client.input.ContentType != "model/gltf-binary" {
		t.Fatalf("unexpected content type %#v", client.input.ContentType)
	}
}

func TestWriterProviderUsesBareFilenameWithoutPrefix(t *testing.T) {
	client := &fakeS3Client{}
	wp := NewWriterProvider(context.Background(), client, "bucket", "")

	w, err := wp("/root/tileset.json")
	if err != nil {
		t.Fatalf("writer provider: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close/upload: %v", err)
	}

	if client.input == nil {
		t.Fatal("expected PutObject to be called")
	}
	if got := *client.input.Key; got != "root/tileset.json" {
		t.Fatalf("unexpected key %q", got)
	}
	if client.input.ContentType != nil {
		t.Fatalf("expected no content type, got %q", *client.input.ContentType)
	}
}

func TestWriterProviderPropagatesUploadError(t *testing.T) {
	wantErr := errors.New("upload failed")
	client := &fakeS3Client{err: wantErr}
	wp := NewWriterProvider(context.Background(), client, "bucket", "tiles")

	w, err := wp("tile.glb")
	if err != nil {
		t.Fatalf("writer provider: %v", err)
	}
	if gotErr := w.Close(); !errors.Is(gotErr, wantErr) {
		t.Fatalf("expected upload error %v, got %v", wantErr, gotErr)
	}
}
