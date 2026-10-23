package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func main() {
	bucket := flag.String("bucket", "", "Destination bucket")
	filePath := flag.String("file", "", "Local file path")
	folderPath := flag.String("folder", "", "Local folder path")
	flag.Parse()

	if *bucket == "" || (*filePath == "") == (*folderPath == "") {
		fmt.Fprintln(os.Stderr, "bucket is required; specify either file or folder")
		os.Exit(1)
	}

	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "unable to load config:", err)
		os.Exit(1)
	}

	client := s3.NewFromConfig(cfg)

	uploadSingleFile := func(localPath string) error {
		objectKey := path.Join("sources", strings.TrimPrefix(filepath.ToSlash(filepath.Clean(localPath)), "dataset/"))
		f, err := os.Open(localPath)
		if err != nil {
			return fmt.Errorf("open file %q: %w", localPath, err)
		}
		defer f.Close()

		_, err = client.PutObject(ctx, &s3.PutObjectInput{
			Bucket: aws.String(*bucket),
			Key:    aws.String(objectKey),
			Body:   f,
		})

		if err != nil {
			return fmt.Errorf("upload %q to %q: %w", localPath, objectKey, err)
		}

		return nil

	}

	// upload single file
	if *folderPath == "" {
		err = uploadSingleFile(*filePath)
		if err != nil {
			fmt.Fprint(os.Stderr, err)
			os.Exit(1)
		}

		return
	}

	folderInfo, err := os.Stat(*folderPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read folder:", err)
		os.Exit(1)
	}
	if !folderInfo.IsDir() {
		fmt.Fprintln(os.Stderr, "folder must identify a directory")
		os.Exit(1)
	}

	err = filepath.WalkDir(*folderPath, func(localPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		name := strings.ToLower(entry.Name())
		if !slices.Contains([]string{".csv", ".md", ".txt", ".json"}, filepath.Ext(name)) {
			return nil
		}
		return uploadSingleFile(localPath)
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "upload folder:", err)
		os.Exit(1)
	}
}
