package cachemanager

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewInMemoryCacheManager(t *testing.T) {
	require.NotPanics(t, func() {
		NewInMemoryCacheManager[string, string]("test", DefaultExpiration, DefaultCleanupInterval)
	})
}

type ExampleStruct struct {
	ID   int
	Name string
}

func TestNewInMemoryCacheManager_GetExistingValue_StructType(t *testing.T) {
	cache := NewInMemoryCacheManager[string, ExampleStruct]("food-cache", DefaultExpiration, DefaultCleanupInterval)
	example := ExampleStruct{
		Name: "apple",
	}
	cache.Set(context.Background(), "ex:1", example, DefaultExpiration)

	got, ok := cache.Get(context.Background(), "ex:1")
	require.True(t, ok)
	require.Equal(t, example, got)
}

func TestNewInMemoryCacheManager_GetExistingValue(t *testing.T) {
	cache := NewInMemoryCacheManager[string, string]("food-cache", DefaultExpiration, DefaultCleanupInterval)
	cache.Set(context.Background(), "food", "apple", DefaultExpiration)

	got, ok := cache.Get(context.Background(), "food")
	require.True(t, ok)
	require.Equal(t, "apple", got)
}

func TestNewInMemoryCacheManager_GetWithNoExistingValue(t *testing.T) {
	cache := NewInMemoryCacheManager[string, string]("food-cache", DefaultExpiration, DefaultCleanupInterval)

	got, ok := cache.Get(context.Background(), "food")
	require.False(t, ok)
	require.Empty(t, got)
}

func TestNewInMemoryCacheManager_GetWithExistingInvalidValueType(t *testing.T) {
	cache := NewInMemoryCacheManager[string, string]("food-cache", DefaultExpiration, DefaultCleanupInterval)

	cache.cache.Set("food", 123, DefaultExpiration)

	got, ok := cache.Get(context.Background(), "food")
	require.False(t, ok)
	require.Empty(t, got)
}

func TestNewInMemoryCacheManager_GetMultipleWithNoKeysDoesNothing(t *testing.T) {
	cache := NewInMemoryCacheManager[string, string]("food-cache", DefaultExpiration, DefaultCleanupInterval)

	got, ok := cache.GetMultiple(context.Background(), []string{})
	require.False(t, ok)
	require.Nil(t, got)
}

func TestNewInMemoryCacheManager_GetMultipleCacheHit(t *testing.T) {
	cache := NewInMemoryCacheManager[string, string]("food-cache", DefaultExpiration, DefaultCleanupInterval)

	cache.cache.Set("food", "apple", DefaultExpiration)
	cache.cache.Set("drink", "juice", DefaultExpiration)

	got, ok := cache.GetMultiple(context.Background(), []string{"food", "drink", "missing"})
	require.True(t, ok)
	require.Equal(t, map[string]string{"food": "apple", "drink": "juice"}, got)
}

func TestNewInMemoryCacheManager_GetMultipleCacheMiss(t *testing.T) {
	cache := NewInMemoryCacheManager[string, string]("food-cache", DefaultExpiration, DefaultCleanupInterval)

	got, ok := cache.GetMultiple(context.Background(), []string{"food", "drink", "missing"})
	require.False(t, ok)
	require.Nil(t, got)
}

func TestNewInMemoryCacheManager_GetMultipleWithExistingInvalidValueType(t *testing.T) {
	cache := NewInMemoryCacheManager[string, string]("food-cache", DefaultExpiration, DefaultCleanupInterval)

	cache.cache.Set("food", "apple", DefaultExpiration)
	cache.cache.Set("drink", 123, DefaultExpiration)

	got, ok := cache.GetMultiple(context.Background(), []string{"food", "drink"})
	require.True(t, ok)
	require.Equal(t, map[string]string{"food": "apple"}, got)
}

func TestNewInMemoryCacheManager_GetWithRefresh_WithNoExistingValue(t *testing.T) {
	cache := NewInMemoryCacheManager[string, string]("food-cache", DefaultExpiration, DefaultCleanupInterval)

	got, ok := cache.GetWithRefresh(context.Background(), "food", time.Minute*60)
	require.False(t, ok)
	require.Equal(t, "", got)
}

func TestNewInMemoryCacheManager_GetWithRefresh_WithExistingValue(t *testing.T) {
	cache := NewInMemoryCacheManager[string, string]("food-cache", DefaultExpiration, DefaultCleanupInterval)
	cache.Set(context.Background(), "food", "apple", DefaultExpiration)

	got, ok := cache.GetWithRefresh(context.Background(), "food", time.Minute*60)
	require.True(t, ok)
	require.Equal(t, "apple", got)
}

func TestNewInMemoryCacheManager_DeleteWithNoKeysDoesNothing(t *testing.T) {
	cache := NewInMemoryCacheManager[string, string]("food-cache", DefaultExpiration, DefaultCleanupInterval)

	err := cache.Delete(context.Background())
	require.NoError(t, err)
}

func TestNewInMemoryCacheManager_DeleteExistingValue(t *testing.T) {
	cache := NewInMemoryCacheManager[string, string]("food-cache", DefaultExpiration, DefaultCleanupInterval)
	cache.Set(context.Background(), "food", "apple", DefaultExpiration)

	got, ok := cache.Get(context.Background(), "food")
	require.True(t, ok)
	require.Equal(t, "apple", got)

	err := cache.Delete(context.Background(), "food")
	require.NoError(t, err)

	got, ok = cache.Get(context.Background(), "food")
	require.False(t, ok)
	require.Equal(t, "", got)
}

func TestNewInMemoryCacheManager_Flush(t *testing.T) {
	cache := NewInMemoryCacheManager[string, string]("food-cache", DefaultExpiration, DefaultCleanupInterval)
	cache.Set(context.Background(), "food", "apple", DefaultExpiration)

	got, ok := cache.Get(context.Background(), "food")
	require.True(t, ok)
	require.Equal(t, "apple", got)

	err := cache.Flush(context.Background())
	require.NoError(t, err)

	got, ok = cache.Get(context.Background(), "food")
	require.False(t, ok)
	require.Equal(t, "", got)
}

func TestNewInMemoryCacheManager_ConcurrentAccess(t *testing.T) {
	cache := NewInMemoryCacheManager[string, string]("concurrent-cache", DefaultExpiration, DefaultCleanupInterval)
	ctx := context.Background()

	// Set initial values
	for i := 0; i < 100; i++ {
		cache.Set(ctx, fmt.Sprintf("key-%d", i), fmt.Sprintf("value-%d", i), DefaultExpiration)
	}

	// Concurrent reads and writes
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				key := fmt.Sprintf("key-%d", (id*10+j)%100)
				_, _ = cache.Get(ctx, key)
				cache.Set(ctx, key, fmt.Sprintf("updated-%d-%d", id, j), DefaultExpiration)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should not panic or deadlock
}

func TestNewInMemoryCacheManager_ConcurrentGetMultiple(t *testing.T) {
	cache := NewInMemoryCacheManager[string, string]("concurrent-multi-cache", DefaultExpiration, DefaultCleanupInterval)
	ctx := context.Background()

	// Set initial values
	keys := make([]string, 50)
	for i := 0; i < 50; i++ {
		key := fmt.Sprintf("key-%d", i)
		keys[i] = key
		cache.Set(ctx, key, fmt.Sprintf("value-%d", i), DefaultExpiration)
	}

	// Concurrent GetMultiple and updates
	done := make(chan bool)
	for i := 0; i < 5; i++ {
		go func(id int) {
			for j := 0; j < 20; j++ {
				_, _ = cache.GetMultiple(ctx, keys)
				cache.Set(ctx, keys[id*10%50], fmt.Sprintf("updated-%d-%d", id, j), DefaultExpiration)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 5; i++ {
		<-done
	}

	// Should not panic or deadlock
}

func TestNewInMemoryCacheManager_DeleteMultiple(t *testing.T) {
	cache := NewInMemoryCacheManager[string, string]("delete-multi-cache", DefaultExpiration, DefaultCleanupInterval)
	ctx := context.Background()

	// Set multiple values
	cache.Set(ctx, "key1", "value1", DefaultExpiration)
	cache.Set(ctx, "key2", "value2", DefaultExpiration)
	cache.Set(ctx, "key3", "value3", DefaultExpiration)

	// Delete multiple keys at once
	err := cache.Delete(ctx, "key1", "key2")
	require.NoError(t, err)

	// Verify deleted keys are gone
	_, ok := cache.Get(ctx, "key1")
	require.False(t, ok)
	_, ok = cache.Get(ctx, "key2")
	require.False(t, ok)

	// key3 should still exist
	got, ok := cache.Get(ctx, "key3")
	require.True(t, ok)
	require.Equal(t, "value3", got)
}

func TestNewInMemoryCacheManager_SetOverwrite(t *testing.T) {
	cache := NewInMemoryCacheManager[string, string]("overwrite-cache", DefaultExpiration, DefaultCleanupInterval)
	ctx := context.Background()

	// Set initial value
	cache.Set(ctx, "key", "original", DefaultExpiration)

	got, ok := cache.Get(ctx, "key")
	require.True(t, ok)
	require.Equal(t, "original", got)

	// Overwrite with new value
	cache.Set(ctx, "key", "updated", DefaultExpiration)

	got, ok = cache.Get(ctx, "key")
	require.True(t, ok)
	require.Equal(t, "updated", got)
}

func TestNewInMemoryCacheManager_PointerType(t *testing.T) {
	type TestStruct struct {
		ID   int
		Name string
	}

	cache := NewInMemoryCacheManager[string, *TestStruct]("pointer-cache", DefaultExpiration, DefaultCleanupInterval)
	ctx := context.Background()

	original := &TestStruct{ID: 1, Name: "test"}
	cache.Set(ctx, "ptr", original, DefaultExpiration)

	got, ok := cache.Get(ctx, "ptr")
	require.True(t, ok)
	require.NotNil(t, got)
	require.Equal(t, 1, got.ID)
	require.Equal(t, "test", got.Name)

	// Verify it's the same pointer
	require.Same(t, original, got)
}
