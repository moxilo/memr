package main

import (
	"context"
	"fmt"
	"io"
	"log"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/golang/snappy"
)

func S3Writer(reader io.ReadCloser, compress bool, region, bucket, key string, concurrency int) (*manager.UploadOutput, error) {

	cfg, err := config.LoadDefaultConfig(context.TODO(),
		// client will only use this region if none is otherwise set using AWS_REGION or AWS_DEFAULT_REGION
		config.WithDefaultRegion(region),
	)
	if err != nil {
		return nil, err
	}

	// Create an uploader with the session and custom options
	uploader := manager.NewUploader(s3.NewFromConfig(cfg), func(u *manager.Uploader) {
		u.PartSize = manager.MinUploadPartSize // 5MB (smallest allowed)
		// 5 goroutines is the default. A lower value here will have a lesser
		// impact on memory pressure, and should be considered
		// Increasing this value will directly impact how much memory we use
		// set to manager.DefaultUploadConcurrency by default
		u.Concurrency = concurrency

		// A buffer provider could be used to allow for larger buffers (64 KiB?) in memory
		// u.BufferProvider = manager.NewBufferedReadSeekerWriteToPool(64 * 1024)
	})

	var s3Reader io.Reader = reader
	if compress {
		var wPipe *io.PipeWriter
		s3Reader, wPipe = io.Pipe()

		go func() {
			writer := snappy.NewBufferedWriter(wPipe)
			defer func() {
				cErr := reader.Close()
				if cErr != nil {
					log.Fatalf("failed to close reader: %s", cErr)
				}
				cErr = writer.Close()
				if cErr != nil {
					log.Fatalf("failed to close writer: %T; %s", writer, cErr)
				}
				cErr = wPipe.Close()
				if cErr != nil {
					log.Fatalf("failed to close pipe writer: %s", cErr)
				}
			}()

			if _, err := io.Copy(writer, reader); err != nil {
				log.Fatalf("compressor failed: %s", err)
			}
		}()
	} else {
		defer reader.Close()
	}

	// Upload the file to S3
	result, err := uploader.Upload(context.TODO(),
		&s3.PutObjectInput{
			ACL:    types.ObjectCannedACLBucketOwnerFullControl,
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
			Body:   s3Reader,
		},
	)

	if err != nil {
		return nil, fmt.Errorf("failed to upload to s3: %s", err)
	}

	return result, nil
}
