package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/interpt-co/flume/internal/models"
)

// --- Mock S3 client ---

type mockS3Client struct {
	mu      sync.Mutex
	objects map[string][]byte // key -> body
}

func newMockS3Client() *mockS3Client {
	return &mockS3Client{objects: make(map[string][]byte)}
}

func (m *mockS3Client) PutObject(_ context.Context, input *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	data, err := io.ReadAll(input.Body)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.objects[aws.ToString(input.Key)] = data
	m.mu.Unlock()
	return &s3.PutObjectOutput{}, nil
}

func (m *mockS3Client) ListObjectsV2(_ context.Context, input *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	prefix := aws.ToString(input.Prefix)
	m.mu.Lock()
	defer m.mu.Unlock()

	var contents []s3types.Object
	for key := range m.objects {
		if strings.HasPrefix(key, prefix) {
			contents = append(contents, s3types.Object{
				Key: aws.String(key),
			})
		}
	}
	// Sort for deterministic output.
	sort.Slice(contents, func(i, j int) bool {
		return aws.ToString(contents[i].Key) < aws.ToString(contents[j].Key)
	})

	return &s3.ListObjectsV2Output{
		Contents:    contents,
		IsTruncated: aws.Bool(false),
	}, nil
}

func (m *mockS3Client) GetObject(_ context.Context, input *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	key := aws.ToString(input.Key)
	m.mu.Lock()
	data, ok := m.objects[key]
	m.mu.Unlock()
	if !ok {
		return nil, &s3types.NoSuchKey{Message: aws.String("not found: " + key)}
	}
	return &s3.GetObjectOutput{
		Body: io.NopCloser(bytes.NewReader(data)),
	}, nil
}

// Verify mock implements S3Client.
var _ S3Client = (*mockS3Client)(nil)

// --- Helper ---

func makeMsg(content string, ts time.Time) models.LogMessage {
	return models.LogMessage{
		ID:        content,
		Content:   content,
		Timestamp: ts,
		Source:    models.SourceStdin,
		Origin:    models.Origin{Name: "test"},
	}
}

// --- Tests ---

func TestChunkKey(t *testing.T) {
	ts := time.Date(2026, 3, 5, 14, 30, 45, 0, time.UTC)
	got := ChunkKey("logs/prod", ts)

	if !strings.HasPrefix(got, "logs/prod/2026/03/05/14/chunk-") {
		t.Errorf("unexpected prefix: %s", got)
	}
	if !strings.HasSuffix(got, ".json.gz") {
		t.Errorf("expected .json.gz suffix: %s", got)
	}

	// The millisecond part should match the timestamp.
	wantMs := ts.UnixMilli()
	wantSuffix := ".json.gz"
	idx := strings.LastIndex(got, "chunk-")
	chunkPart := got[idx:]
	var ms int64
	n, err := parseChunkMs(chunkPart)
	if err != nil {
		t.Fatalf("failed to parse chunk ms: %v", err)
	}
	ms = n
	if ms != wantMs {
		t.Errorf("chunk ms = %d, want %d (suffix: %s)", ms, wantMs, wantSuffix)
	}
}

func parseChunkMs(s string) (int64, error) {
	// "chunk-1709647200000.json.gz"
	s = strings.TrimPrefix(s, "chunk-")
	s = strings.TrimSuffix(s, ".json.gz")
	var ms int64
	for _, c := range s {
		ms = ms*10 + int64(c-'0')
	}
	return ms, nil
}

func TestChunkKey_TrailingSlash(t *testing.T) {
	ts := time.Date(2026, 1, 2, 3, 0, 0, 0, time.UTC)
	got := ChunkKey("prefix/", ts)
	if strings.Contains(got, "//") {
		t.Errorf("double slash in key: %s", got)
	}
}

func TestHourPrefix(t *testing.T) {
	ts := time.Date(2026, 3, 5, 14, 30, 0, 0, time.UTC)
	got := HourPrefix("logs", ts)
	want := "logs/2026/03/05/14/"
	if got != want {
		t.Errorf("HourPrefix = %q, want %q", got, want)
	}
}

func TestMarshalUnmarshalGzip_RoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	msgs := []models.LogMessage{
		makeMsg("alpha", now),
		makeMsg("beta", now.Add(-time.Second)),
		{
			ID:          "json-msg",
			Content:     `{"key":"val"}`,
			JsonContent: json.RawMessage(`{"key":"val"}`),
			IsJson:      true,
			Timestamp:   now.Add(-2 * time.Second),
			Source:      models.SourceLoki,
			Origin:      models.Origin{Name: "loki", Meta: map[string]string{"job": "app"}},
			Labels:      map[string]string{"env": "prod"},
			Level:       "error",
		},
	}

	data, err := MarshalGzip(msgs)
	if err != nil {
		t.Fatalf("MarshalGzip: %v", err)
	}

	got, err := UnmarshalGzip(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("UnmarshalGzip: %v", err)
	}

	if len(got) != len(msgs) {
		t.Fatalf("round-trip len = %d, want %d", len(got), len(msgs))
	}

	for i := range msgs {
		if got[i].ID != msgs[i].ID {
			t.Errorf("[%d] ID = %q, want %q", i, got[i].ID, msgs[i].ID)
		}
		if got[i].Content != msgs[i].Content {
			t.Errorf("[%d] Content = %q, want %q", i, got[i].Content, msgs[i].Content)
		}
		if !got[i].Timestamp.Equal(msgs[i].Timestamp) {
			t.Errorf("[%d] Timestamp = %v, want %v", i, got[i].Timestamp, msgs[i].Timestamp)
		}
		if got[i].Source != msgs[i].Source {
			t.Errorf("[%d] Source = %q, want %q", i, got[i].Source, msgs[i].Source)
		}
		if got[i].IsJson != msgs[i].IsJson {
			t.Errorf("[%d] IsJson = %v, want %v", i, got[i].IsJson, msgs[i].IsJson)
		}
		if got[i].Level != msgs[i].Level {
			t.Errorf("[%d] Level = %q, want %q", i, got[i].Level, msgs[i].Level)
		}
	}
}

func TestMarshalGzip_EmptySlice(t *testing.T) {
	data, err := MarshalGzip([]models.LogMessage{})
	if err != nil {
		t.Fatalf("MarshalGzip empty: %v", err)
	}
	got, err := UnmarshalGzip(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("UnmarshalGzip empty: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %d items", len(got))
	}
}

func TestS3Storage_FlushOnCount(t *testing.T) {
	mock := newMockS3Client()
	cfg := S3Config{
		Bucket:        "test-bucket",
		Prefix:        "ns",
		FlushInterval: time.Hour, // long interval so only count triggers flush
		FlushCount:    3,
	}
	stor := NewS3StorageWithClient(mock, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- stor.Start(ctx) }()

	now := time.Now().UTC()
	// Send exactly FlushCount messages.
	for i := 0; i < 3; i++ {
		stor.Writer() <- makeMsg("msg", now.Add(time.Duration(i)*time.Millisecond))
	}

	// Wait a bit for the flush to happen.
	time.Sleep(200 * time.Millisecond)

	mock.mu.Lock()
	numObjects := len(mock.objects)
	mock.mu.Unlock()

	if numObjects != 1 {
		t.Errorf("expected 1 S3 object after count flush, got %d", numObjects)
	}

	cancel()
	<-errCh
}

func TestS3Storage_FlushOnInterval(t *testing.T) {
	mock := newMockS3Client()
	cfg := S3Config{
		Bucket:        "test-bucket",
		Prefix:        "ns",
		FlushInterval: 100 * time.Millisecond,
		FlushCount:    10000, // high count so only interval triggers flush
	}
	stor := NewS3StorageWithClient(mock, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- stor.Start(ctx) }()

	stor.Writer() <- makeMsg("hello", time.Now())

	// Wait for at least one interval tick + processing time.
	time.Sleep(300 * time.Millisecond)

	mock.mu.Lock()
	numObjects := len(mock.objects)
	mock.mu.Unlock()

	if numObjects != 1 {
		t.Errorf("expected 1 S3 object after interval flush, got %d", numObjects)
	}

	cancel()
	<-errCh
}

func TestS3Storage_FlushOnShutdown(t *testing.T) {
	mock := newMockS3Client()
	cfg := S3Config{
		Bucket:        "test-bucket",
		Prefix:        "ns",
		FlushInterval: time.Hour,
		FlushCount:    10000,
	}
	stor := NewS3StorageWithClient(mock, cfg)

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() { errCh <- stor.Start(ctx) }()

	stor.Writer() <- makeMsg("leftover", time.Now())

	// Give the message time to be received by the loop.
	time.Sleep(50 * time.Millisecond)

	// Cancel context — should trigger shutdown flush.
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Start returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return in time")
	}

	mock.mu.Lock()
	numObjects := len(mock.objects)
	mock.mu.Unlock()

	if numObjects != 1 {
		t.Errorf("expected 1 S3 object after shutdown flush, got %d", numObjects)
	}
}

func TestS3Storage_ReadBefore(t *testing.T) {
	mock := newMockS3Client()
	cfg := S3Config{
		Bucket: "test-bucket",
		Prefix: "ns",
	}

	// Seed the mock with two chunks in the same hour.
	now := time.Date(2026, 3, 5, 14, 30, 0, 0, time.UTC)

	chunk1Msgs := []models.LogMessage{
		makeMsg("a", now.Add(-10*time.Minute)),
		makeMsg("b", now.Add(-5*time.Minute)),
	}
	chunk2Msgs := []models.LogMessage{
		makeMsg("c", now.Add(-2*time.Minute)),
		makeMsg("d", now.Add(-1*time.Minute)),
	}

	data1, _ := MarshalGzip(chunk1Msgs)
	data2, _ := MarshalGzip(chunk2Msgs)

	key1 := ChunkKey("ns", now.Add(-10*time.Minute))
	key2 := ChunkKey("ns", now.Add(-2*time.Minute))

	mock.objects[key1] = data1
	mock.objects[key2] = data2

	stor := NewS3StorageWithClient(mock, cfg)

	// Read all messages before now.
	msgs, err := stor.ReadBefore(context.Background(), now, 10)
	if err != nil {
		t.Fatalf("ReadBefore: %v", err)
	}

	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(msgs))
	}

	// Verify newest-first ordering.
	for i := 1; i < len(msgs); i++ {
		if msgs[i].Timestamp.After(msgs[i-1].Timestamp) {
			t.Errorf("messages not in reverse chronological order at index %d", i)
		}
	}
}

func TestS3Storage_ReadBefore_LimitCount(t *testing.T) {
	mock := newMockS3Client()
	cfg := S3Config{
		Bucket: "test-bucket",
		Prefix: "ns",
	}

	now := time.Date(2026, 3, 5, 14, 30, 0, 0, time.UTC)

	msgs := make([]models.LogMessage, 5)
	for i := range msgs {
		msgs[i] = makeMsg("m", now.Add(-time.Duration(i+1)*time.Minute))
	}
	data, _ := MarshalGzip(msgs)
	mock.objects[ChunkKey("ns", now)] = data

	stor := NewS3StorageWithClient(mock, cfg)

	got, err := stor.ReadBefore(context.Background(), now, 2)
	if err != nil {
		t.Fatalf("ReadBefore: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 messages, got %d", len(got))
	}
}

func TestS3Storage_ReadBefore_CrossHour(t *testing.T) {
	mock := newMockS3Client()
	cfg := S3Config{
		Bucket: "test-bucket",
		Prefix: "ns",
	}

	// Messages in hour 14.
	hour14 := time.Date(2026, 3, 5, 14, 10, 0, 0, time.UTC)
	data14, _ := MarshalGzip([]models.LogMessage{
		makeMsg("h14", hour14),
	})
	mock.objects[ChunkKey("ns", hour14)] = data14

	// Messages in hour 13.
	hour13 := time.Date(2026, 3, 5, 13, 50, 0, 0, time.UTC)
	data13, _ := MarshalGzip([]models.LogMessage{
		makeMsg("h13", hour13),
	})
	mock.objects[ChunkKey("ns", hour13)] = data13

	stor := NewS3StorageWithClient(mock, cfg)

	// Ask for messages before 14:30, want 10 — should get both hours.
	before := time.Date(2026, 3, 5, 14, 30, 0, 0, time.UTC)
	got, err := stor.ReadBefore(context.Background(), before, 10)
	if err != nil {
		t.Fatalf("ReadBefore: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("expected 2 messages across hours, got %d", len(got))
	}

	// Newest first.
	if got[0].Content != "h14" {
		t.Errorf("first message should be h14, got %q", got[0].Content)
	}
	if got[1].Content != "h13" {
		t.Errorf("second message should be h13, got %q", got[1].Content)
	}
}

func TestS3Storage_ReadBefore_FiltersFuture(t *testing.T) {
	mock := newMockS3Client()
	cfg := S3Config{
		Bucket: "test-bucket",
		Prefix: "ns",
	}

	now := time.Date(2026, 3, 5, 14, 30, 0, 0, time.UTC)

	// A chunk with one message before and one after the cutoff.
	msgs := []models.LogMessage{
		makeMsg("past", now.Add(-5*time.Minute)),
		makeMsg("future", now.Add(5*time.Minute)),
	}
	data, _ := MarshalGzip(msgs)
	mock.objects[ChunkKey("ns", now)] = data

	stor := NewS3StorageWithClient(mock, cfg)

	got, err := stor.ReadBefore(context.Background(), now, 10)
	if err != nil {
		t.Fatalf("ReadBefore: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("expected 1 message (future filtered out), got %d", len(got))
	}
	if got[0].Content != "past" {
		t.Errorf("expected 'past', got %q", got[0].Content)
	}
}

func TestS3Storage_WriteAndReadRoundTrip(t *testing.T) {
	mock := newMockS3Client()
	cfg := S3Config{
		Bucket:        "test-bucket",
		Prefix:        "rt",
		FlushInterval: 50 * time.Millisecond,
		FlushCount:    100,
	}
	stor := NewS3StorageWithClient(mock, cfg)

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() { errCh <- stor.Start(ctx) }()

	now := time.Now().UTC().Truncate(time.Millisecond)
	sent := []models.LogMessage{
		makeMsg("one", now.Add(-3*time.Second)),
		makeMsg("two", now.Add(-2*time.Second)),
		makeMsg("three", now.Add(-1*time.Second)),
	}
	for _, m := range sent {
		stor.Writer() <- m
	}

	// Wait for interval flush.
	time.Sleep(200 * time.Millisecond)
	cancel()
	<-errCh

	// Now read back.
	got, err := stor.ReadBefore(context.Background(), now, 10)
	if err != nil {
		t.Fatalf("ReadBefore: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(got))
	}

	// Verify newest-first.
	if got[0].Content != "three" {
		t.Errorf("first should be 'three', got %q", got[0].Content)
	}
	if got[2].Content != "one" {
		t.Errorf("last should be 'one', got %q", got[2].Content)
	}
}

func TestS3Storage_InterfaceCompliance(t *testing.T) {
	// Compile-time check is already done via var _ Storage = (*S3Storage)(nil).
	// This test verifies the methods exist with the expected signatures.
	mock := newMockS3Client()
	cfg := S3Config{Bucket: "b", Prefix: "p"}
	var s Storage = NewS3StorageWithClient(mock, cfg)

	_ = s.Writer()
	_, _ = s.ReadBefore(context.Background(), time.Now(), 10)
}

func TestNewS3StorageWithClient_Defaults(t *testing.T) {
	mock := newMockS3Client()
	cfg := S3Config{
		Bucket: "b",
		Prefix: "p",
	}
	stor := NewS3StorageWithClient(mock, cfg)

	if stor.cfg.FlushInterval != 10*time.Second {
		t.Errorf("default FlushInterval = %v, want 10s", stor.cfg.FlushInterval)
	}
	if stor.cfg.FlushCount != 1000 {
		t.Errorf("default FlushCount = %d, want 1000", stor.cfg.FlushCount)
	}
}
