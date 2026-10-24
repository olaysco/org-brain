package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func TestSourceKeysPagination(t *testing.T) {
	requests := 0
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Query().Get("prefix") != "sources/logistics/" {
			t.Errorf("unexpected prefix %q", r.URL.Query().Get("prefix"))
		}
		w.Header().Set("Content-Type", "application/xml")
		switch r.URL.Query().Get("continuation-token") {
		case "":
			fmt.Fprint(w, `<ListBucketResult><IsTruncated>true</IsTruncated><NextContinuationToken>next</NextContinuationToken><Contents><Key>sources/logistics/shipments.csv</Key></Contents><Contents><Key>sources/logistics/shipments.csv.metadata.json</Key></Contents><Contents><Key>sources/logistics/image.png</Key></Contents></ListBucketResult>`)
		case "next":
			fmt.Fprint(w, `<ListBucketResult><IsTruncated>false</IsTruncated><Contents><Key>sources/logistics/nested/routes.CSV</Key></Contents><Contents><Key>sources/logistics/procedure.md</Key></Contents><Contents><Key>sources/logistics-old/other.csv</Key></Contents></ListBucketResult>`)
		default:
			t.Error("unexpected continuation token")
		}
	}))
	defer backend.Close()
	client := s3.NewFromConfig(aws.Config{Region: "eu-west-2", Credentials: credentials.NewStaticCredentialsProvider("test", "test", "")}, func(o *s3.Options) { o.BaseEndpoint = aws.String(backend.URL); o.UsePathStyle = true })
	keys, err := sourceKeys(context.Background(), client, "demo", "", "sources/logistics")
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{"sources/logistics/shipments.csv", "sources/logistics/nested/routes.CSV", "sources/logistics/procedure.md"}
	if !reflect.DeepEqual(keys, expected) || requests != 2 {
		t.Fatalf("keys=%v; requests=%d", keys, requests)
	}
}

func TestSourceKeysSingleDocument(t *testing.T) {
	keys, err := sourceKeys(context.Background(), nil, "demo", "sources/logistics/shipments.csv", "")
	if err != nil || !reflect.DeepEqual(keys, []string{"sources/logistics/shipments.csv"}) {
		t.Fatalf("keys=%v; error=%v", keys, err)
	}
	if _, err := sourceKeys(context.Background(), nil, "demo", "sources/logistics/shipments.csv.metadata.json", ""); err == nil {
		t.Fatal("metadata accepted as a source")
	}
}

func TestSourceKeysEmptyPrefix(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprint(w, `<ListBucketResult><IsTruncated>false</IsTruncated></ListBucketResult>`)
	}))
	defer backend.Close()
	client := s3.NewFromConfig(aws.Config{Region: "eu-west-2", Credentials: credentials.NewStaticCredentialsProvider("test", "test", "")}, func(o *s3.Options) { o.BaseEndpoint = aws.String(backend.URL); o.UsePathStyle = true })
	if _, err := sourceKeys(context.Background(), client, "demo", "", "sources/logistics/"); err == nil {
		t.Fatal("empty source list accepted")
	}
}
