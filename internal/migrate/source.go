package migrate

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/x/mongo/driver/connstring"
)

// Source 是迁移的数据来源：一个 MongoDB 数据库，或 mongodump 导出的目录。
type Source interface {
	// Describe 返回来源的说明，用于报告（不含密码）。
	Describe() string
	// Collections 列出全部集合。
	Collections(ctx context.Context) ([]string, error)
	// Count 返回集合的文档数，集合不存在时为 0。
	Count(ctx context.Context, coll string) (int64, error)
	// Each 按自然顺序（与 YApi 不带排序的查询相同）遍历集合，集合不存在时什么也不做。
	Each(ctx context.Context, coll string, fn func(bson.D) error) error
	Close(ctx context.Context) error
}

// ---- MongoDB ----

type mongoSource struct {
	client *mongo.Client
	db     *mongo.Database
	desc   string
}

// OpenMongo 连接 MongoDB。dbName 为空时取连接串里的数据库名，再没有就用 YApi 的默认值 yapi。
// 使用的驱动支持 MongoDB 3.6 及以上版本；更早的版本请先用 mongodump 导出，再用 OpenDump。
func OpenMongo(ctx context.Context, uri, dbName string) (Source, error) {
	cs, err := connstring.ParseAndValidate(uri)
	if err != nil {
		return nil, fmt.Errorf("MongoDB 连接串格式不对: %w", err)
	}
	if dbName == "" {
		dbName = cs.Database
	}
	if dbName == "" {
		dbName = "yapi"
	}
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri).SetServerSelectionTimeout(10*time.Second))
	if err != nil {
		return nil, err
	}
	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(ctx)
		return nil, fmt.Errorf("连接 MongoDB 失败（低于 3.6 的版本请改用 -dump）: %w", err)
	}
	return &mongoSource{
		client: client,
		db:     client.Database(dbName),
		desc:   fmt.Sprintf("MongoDB %s，数据库 %s", strings.Join(cs.Hosts, ","), dbName),
	}, nil
}

func (s *mongoSource) Describe() string { return s.desc }

func (s *mongoSource) Collections(ctx context.Context) ([]string, error) {
	names, err := s.db.ListCollectionNames(ctx, bson.D{})
	sort.Strings(names)
	return names, err
}

func (s *mongoSource) Count(ctx context.Context, coll string) (int64, error) {
	return s.db.Collection(coll).CountDocuments(ctx, bson.D{})
}

func (s *mongoSource) Each(ctx context.Context, coll string, fn func(bson.D) error) error {
	cur, err := s.db.Collection(coll).Find(ctx, bson.D{})
	if err != nil {
		return err
	}
	defer func() { _ = cur.Close(ctx) }()
	for cur.Next(ctx) {
		var d bson.D
		if err := cur.Decode(&d); err != nil {
			return fmt.Errorf("%s：解码文档失败: %w", coll, err)
		}
		if err := fn(d); err != nil {
			return err
		}
	}
	return cur.Err()
}

func (s *mongoSource) Close(ctx context.Context) error { return s.client.Disconnect(ctx) }

// ---- mongodump 目录 ----

type dumpSource struct {
	dir string
}

// OpenDump 打开 mongodump 导出的目录，可以是数据库目录本身（里面是 user.bson 等文件），
// 也可以是 mongodump --out 指定的上级目录（里面是名为 dbName 的数据库目录，dbName 为空时用 yapi）。
// 支持 --gzip 导出的 .bson.gz 文件。任何版本的 MongoDB 导出的数据都可以读取。
func OpenDump(dir, dbName string) (Source, error) {
	if dbName == "" {
		dbName = "yapi"
	}
	for _, d := range []string{dir, filepath.Join(dir, dbName)} {
		if dumpFile(d, "user") != "" {
			return &dumpSource{dir: d}, nil
		}
	}
	return nil, fmt.Errorf("%s 里没有找到 user.bson，请指定 mongodump 导出的 YApi 数据库目录", dir)
}

// dumpFile 返回集合对应的文件，不存在时为空字符串。
func dumpFile(dir, coll string) string {
	for _, ext := range []string{".bson", ".bson.gz"} {
		f := filepath.Join(dir, coll+ext)
		if _, err := os.Stat(f); err == nil {
			return f
		}
	}
	return ""
}

func (s *dumpSource) Describe() string { return "mongodump 目录 " + s.dir }

func (s *dumpSource) Collections(_ context.Context) ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		name := e.Name()
		for _, ext := range []string{".bson.gz", ".bson"} {
			if strings.HasSuffix(name, ext) {
				names = append(names, strings.TrimSuffix(name, ext))
				break
			}
		}
	}
	sort.Strings(names)
	return names, nil
}

func (s *dumpSource) Count(ctx context.Context, coll string) (int64, error) {
	var n int64
	err := s.Each(ctx, coll, func(bson.D) error { n++; return nil })
	return n, err
}

func (s *dumpSource) Each(ctx context.Context, coll string, fn func(bson.D) error) error {
	file := dumpFile(s.dir, coll)
	if file == "" {
		return nil
	}
	f, err := os.Open(file) // #nosec G304 -- 文件在用户指定的导出目录里
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	var r io.Reader = f
	if strings.HasSuffix(file, ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
		defer func() { _ = gz.Close() }()
		r = gz
	}
	return readBSON(ctx, bufio.NewReaderSize(r, 1<<20), fn, file)
}

func (s *dumpSource) Close(context.Context) error { return nil }

// readBSON 逐条读取连续存放的 BSON 文档（mongodump 的 .bson 文件格式：每条文档以 4 字节小端长度开头）。
func readBSON(ctx context.Context, r io.Reader, fn func(bson.D) error, name string) error {
	const maxDoc = 16 << 20 // MongoDB 单个文档的上限
	var head [4]byte
	for i := 0; ; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := io.ReadFull(r, head[:]); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("%s 第 %d 条文档: %w", name, i+1, err)
		}
		size := binary.LittleEndian.Uint32(head[:])
		if size < 5 || size > maxDoc {
			return fmt.Errorf("%s 第 %d 条文档长度 %d 不对，文件可能已损坏", name, i+1, size)
		}
		raw := make([]byte, size)
		copy(raw, head[:])
		if _, err := io.ReadFull(r, raw[4:]); err != nil {
			return fmt.Errorf("%s 第 %d 条文档不完整: %w", name, i+1, err)
		}
		var d bson.D
		if err := bson.Unmarshal(raw, &d); err != nil {
			return fmt.Errorf("%s 第 %d 条文档: %w", name, i+1, err)
		}
		if err := fn(d); err != nil {
			return err
		}
	}
}
