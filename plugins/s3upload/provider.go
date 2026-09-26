package s3upload

import (
	"bytes"
	"context"
	"io"
	"path"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/mfbonfigli/gotiler/v3/tiler/plugin"
)

type PutObjectAPI interface {
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
}

type ProviderOptions struct {
	ContentType func(filename string) string
}

type Option func(*ProviderOptions)

func WithContentType(fn func(filename string) string) Option {
	return func(opts *ProviderOptions) {
		opts.ContentType = fn
	}
}

func NewWriterProvider(ctx context.Context, client PutObjectAPI, bucket, prefix string, options ...Option) plugin.WriterProvider {
	opts := ProviderOptions{}
	for _, opt := range options {
		opt(&opts)
	}
	return func(filename string) (io.WriteCloser, error) {
		key := path.Join(prefix, filename)
		key = strings.TrimPrefix(key, "/")
		return &uploadWriter{
			ctx:         ctx,
			client:      client,
			bucket:      bucket,
			key:         key,
			contentType: opts.ContentType,
		}, nil
	}
}

type uploadWriter struct {
	bytes.Buffer
	ctx         context.Context
	client      PutObjectAPI
	bucket      string
	key         string
	contentType func(filename string) string
}

func (w *uploadWriter) Close() error {
	input := &s3.PutObjectInput{
		Bucket: aws.String(w.bucket),
		Key:    aws.String(w.key),
		Body:   bytes.NewReader(w.Bytes()),
	}
	if w.contentType != nil {
		if contentType := w.contentType(w.key); contentType != "" {
			input.ContentType = aws.String(contentType)
		}
	}
	_, err := w.client.PutObject(w.ctx, input)
	return err
}
