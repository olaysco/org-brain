package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func main() {
	bucket := flag.String("bucket", "", "Destination bucket")
	filePath := flag.String("file", "", "Local file path")
	key := flag.String("key", "", "Destination object key")
	flag.Parse()

	if *bucket == "" || *filePath == "" || *key == "" {
		fmt.Fprintln(os.Stderr, "bucket, file, and key are required")
		os.Exit(1)
	}

	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "unable to load config:", err)
		os.Exit(1)
	}

	client := s3.NewFromConfig(cfg)
	f, err := os.Open(*filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to open file %q : %v \n", *filePath, err)
		os.Exit(1)
	}
	defer f.Close()

	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(*bucket),
		Key:    aws.String(*key),
		Body:   f,
	})

	if err != nil {
		fmt.Fprintln(os.Stderr, "unable to upload file:", err)
		os.Exit(1)
	}
}
