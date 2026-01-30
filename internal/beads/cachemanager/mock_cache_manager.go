package cachemanager

import (
	"context"
	"time"
)

// CacheManagerMock is a mock implementation of CacheManager for testing.
// This uses the function-field pattern consistent with moq-generated mocks.
// Note: moq doesn't support generic interfaces, so this is hand-written.
type CacheManagerMock[K comparable, V any] struct {
	GetFunc            func(ctx context.Context, key K) (V, bool)
	GetMultipleFunc    func(ctx context.Context, keys []K) (map[K]V, bool)
	GetWithRefreshFunc func(ctx context.Context, key K, ttl time.Duration) (V, bool)
	SetFunc            func(ctx context.Context, key K, value V, ttl time.Duration)
	DeleteFunc         func(ctx context.Context, keys ...K) error
	FlushFunc          func(ctx context.Context) error
}

// Compile-time check that CacheManagerMock implements CacheManager.
var _ CacheManager[string, any] = (*CacheManagerMock[string, any])(nil)

func (m *CacheManagerMock[K, V]) Get(ctx context.Context, key K) (V, bool) {
	if m.GetFunc != nil {
		return m.GetFunc(ctx, key)
	}
	var zero V
	return zero, false
}

func (m *CacheManagerMock[K, V]) GetMultiple(ctx context.Context, keys []K) (map[K]V, bool) {
	if m.GetMultipleFunc != nil {
		return m.GetMultipleFunc(ctx, keys)
	}
	return nil, false
}

func (m *CacheManagerMock[K, V]) GetWithRefresh(ctx context.Context, key K, ttl time.Duration) (V, bool) {
	if m.GetWithRefreshFunc != nil {
		return m.GetWithRefreshFunc(ctx, key, ttl)
	}
	var zero V
	return zero, false
}

func (m *CacheManagerMock[K, V]) Set(ctx context.Context, key K, value V, ttl time.Duration) {
	if m.SetFunc != nil {
		m.SetFunc(ctx, key, value, ttl)
	}
}

func (m *CacheManagerMock[K, V]) Delete(ctx context.Context, keys ...K) error {
	if m.DeleteFunc != nil {
		return m.DeleteFunc(ctx, keys...)
	}
	return nil
}

func (m *CacheManagerMock[K, V]) Flush(ctx context.Context) error {
	if m.FlushFunc != nil {
		return m.FlushFunc(ctx)
	}
	return nil
}
