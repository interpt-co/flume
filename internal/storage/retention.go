package storage

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	log "github.com/sirupsen/logrus"
)

// RunRetentionLoop periodically deletes S3 objects older than the configured
// retention period. It runs once immediately, then every hour.
func RunRetentionLoop(ctx context.Context, client S3Client, cfg S3Config) {
	log.WithField("retention", cfg.Retention).Info("storage: retention loop started")

	// Run once immediately.
	if err := runRetention(ctx, client, cfg); err != nil {
		log.WithError(err).Warn("storage: retention sweep error")
	}

	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := runRetention(ctx, client, cfg); err != nil {
				log.WithError(err).Warn("storage: retention sweep error")
			}
		}
	}
}

// runRetention performs a single retention sweep: discovers hour-level
// prefixes, parses their timestamps, and deletes all objects under expired ones.
func runRetention(ctx context.Context, client S3Client, cfg S3Config) error {
	cutoff := time.Now().UTC().Add(-cfg.Retention)
	prefix := strings.TrimRight(cfg.Prefix, "/") + "/"

	if cfg.PartitionLabel != "" {
		return runRetentionPartitioned(ctx, client, cfg.Bucket, prefix, cutoff)
	}
	return runRetentionUnpartitioned(ctx, client, cfg.Bucket, prefix, cutoff)
}

// runRetentionUnpartitioned handles prefix/{YYYY}/{MM}/{DD}/{HH}/ layout.
func runRetentionUnpartitioned(ctx context.Context, client S3Client, bucket, prefix string, cutoff time.Time) error {
	hourPrefixes, err := discoverHourPrefixes(ctx, client, bucket, prefix)
	if err != nil {
		return err
	}
	return deleteExpiredPrefixes(ctx, client, bucket, prefix, hourPrefixes, cutoff)
}

// runRetentionPartitioned handles prefix/{partition}/{YYYY}/{MM}/{DD}/{HH}/ layout.
func runRetentionPartitioned(ctx context.Context, client S3Client, bucket, prefix string, cutoff time.Time) error {
	partitions, err := listCommonPrefixes(ctx, client, bucket, prefix)
	if err != nil {
		return err
	}
	for _, partPrefix := range partitions {
		hourPrefixes, err := discoverHourPrefixes(ctx, client, bucket, partPrefix)
		if err != nil {
			return err
		}
		if err := deleteExpiredPrefixes(ctx, client, bucket, partPrefix, hourPrefixes, cutoff); err != nil {
			return err
		}
	}
	return nil
}

// discoverHourPrefixes walks the year/month/day/hour prefix tree using
// delimiter-based listings. Returns full prefixes like "prefix/2026/03/05/14/".
func discoverHourPrefixes(ctx context.Context, client S3Client, bucket, prefix string) ([]string, error) {
	years, err := listCommonPrefixes(ctx, client, bucket, prefix)
	if err != nil {
		return nil, err
	}

	var hours []string
	for _, yearP := range years {
		months, err := listCommonPrefixes(ctx, client, bucket, yearP)
		if err != nil {
			return nil, err
		}
		for _, monthP := range months {
			days, err := listCommonPrefixes(ctx, client, bucket, monthP)
			if err != nil {
				return nil, err
			}
			for _, dayP := range days {
				hourPs, err := listCommonPrefixes(ctx, client, bucket, dayP)
				if err != nil {
					return nil, err
				}
				hours = append(hours, hourPs...)
			}
		}
	}
	return hours, nil
}

// listCommonPrefixes returns the common prefixes (subdirectories) under a prefix.
func listCommonPrefixes(ctx context.Context, client S3Client, bucket, prefix string) ([]string, error) {
	out, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket:    aws.String(bucket),
		Prefix:    aws.String(prefix),
		Delimiter: aws.String("/"),
	})
	if err != nil {
		return nil, fmt.Errorf("listing prefixes under %s: %w", prefix, err)
	}
	var prefixes []string
	for _, cp := range out.CommonPrefixes {
		prefixes = append(prefixes, aws.ToString(cp.Prefix))
	}
	return prefixes, nil
}

// deleteExpiredPrefixes parses timestamps from hour prefixes and deletes all
// objects under expired ones.
func deleteExpiredPrefixes(ctx context.Context, client S3Client, bucket, basePrefix string, hourPrefixes []string, cutoff time.Time) error {
	for _, hp := range hourPrefixes {
		t, err := ParseHourFromPrefix(hp, basePrefix)
		if err != nil {
			log.WithField("prefix", hp).Debug("storage: skipping unparseable prefix")
			continue
		}
		// The entire hour is expired if the end of the hour is at or before the cutoff.
		hourEnd := t.Add(time.Hour)
		if hourEnd.After(cutoff) {
			continue
		}

		if err := deleteAllUnder(ctx, client, bucket, hp); err != nil {
			return fmt.Errorf("deleting expired prefix %s: %w", hp, err)
		}
		log.WithField("prefix", hp).Info("storage: deleted expired hour")
	}
	return nil
}

// ParseHourFromPrefix extracts a time from a prefix like "prefix/2026/03/05/14/"
// by parsing the last four numeric path components as YYYY/MM/DD/HH.
func ParseHourFromPrefix(hourPrefix, basePrefix string) (time.Time, error) {
	rel := strings.TrimPrefix(hourPrefix, basePrefix)
	rel = strings.TrimSuffix(rel, "/")

	parts := strings.Split(rel, "/")
	if len(parts) < 4 {
		return time.Time{}, fmt.Errorf("not enough path components: %q", rel)
	}

	// Take the last 4 components.
	tail := parts[len(parts)-4:]
	year, err := strconv.Atoi(tail[0])
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid year %q: %w", tail[0], err)
	}
	month, err := strconv.Atoi(tail[1])
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid month %q: %w", tail[1], err)
	}
	day, err := strconv.Atoi(tail[2])
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid day %q: %w", tail[2], err)
	}
	hour, err := strconv.Atoi(tail[3])
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid hour %q: %w", tail[3], err)
	}

	return time.Date(year, time.Month(month), day, hour, 0, 0, 0, time.UTC), nil
}

// deleteAllUnder lists all objects under a prefix and deletes them in batches.
func deleteAllUnder(ctx context.Context, client S3Client, bucket, prefix string) error {
	var continuationToken *string

	for {
		out, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(bucket),
			Prefix:            aws.String(prefix),
			ContinuationToken: continuationToken,
		})
		if err != nil {
			return err
		}

		if len(out.Contents) == 0 {
			break
		}

		// Build delete batch (max 1000 per DeleteObjects call).
		ids := make([]s3types.ObjectIdentifier, 0, len(out.Contents))
		for _, obj := range out.Contents {
			ids = append(ids, s3types.ObjectIdentifier{Key: obj.Key})
		}

		_, err = client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(bucket),
			Delete: &s3types.Delete{
				Objects: ids,
				Quiet:   aws.Bool(true),
			},
		})
		if err != nil {
			return fmt.Errorf("DeleteObjects: %w", err)
		}

		if !aws.ToBool(out.IsTruncated) {
			break
		}
		continuationToken = out.NextContinuationToken
	}
	return nil
}
