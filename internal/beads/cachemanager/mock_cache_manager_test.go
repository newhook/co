package cachemanager

import (
	"context"
	"time"

	"github.com/stretchr/testify/mock"
)

// MockCacheManager is a mock implementation of CacheManager for testing
type MockCacheManager[K comparable, V any] struct {
	mock.Mock
}

func (m *MockCacheManager[K, V]) Get(ctx context.Context, key K) (V, bool) {
	args := m.Called(ctx, key)
	return args.Get(0).(V), args.Bool(1)
}

func (m *MockCacheManager[K, V]) GetMultiple(ctx context.Context, keys []K) (map[K]V, bool) {
	args := m.Called(ctx, keys)
	return args.Get(0).(map[K]V), args.Bool(1)
}

func (m *MockCacheManager[K, V]) GetWithRefresh(ctx context.Context, key K, ttl time.Duration) (V, bool) {
	args := m.Called(ctx, key, ttl)
	return args.Get(0).(V), args.Bool(1)
}

func (m *MockCacheManager[K, V]) Set(ctx context.Context, key K, value V, ttl time.Duration) {
	m.Called(ctx, key, value, ttl)
}

func (m *MockCacheManager[K, V]) Delete(ctx context.Context, keys ...K) error {
	args := m.Called(ctx, keys)
	return args.Error(0)
}

func (m *MockCacheManager[K, V]) Flush(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}
