package store

import (
	"context"
	"testing"
)

type parameterFilter interface {
	ParamsFilter(ctx context.Context, sql string, params ...interface{}) (string, []interface{})
}

/**
 * TestProductionLoggerDoesNotExposeParameters 验证生产日志器不会展开 SQL 参数。
 * @param t 当前测试上下文。
 * @returns 无返回值。
 */
func TestProductionLoggerDoesNotExposeParameters(t *testing.T) {
	filter, ok := productionLogger().(parameterFilter)
	if !ok {
		t.Fatal("production logger does not expose the GORM parameter filter")
	}
	sqlText, params := filter.ParamsFilter(
		context.Background(),
		"INSERT INTO document_revisions (doc_json) VALUES (?)",
		"sensitive-base64-payload",
	)
	if sqlText != "INSERT INTO document_revisions (doc_json) VALUES (?)" {
		t.Fatalf("logger changed parameterized SQL: %s", sqlText)
	}
	if len(params) != 0 {
		t.Fatalf("logger retained %d SQL parameters", len(params))
	}
}
