package main

import "testing"

func TestInsert(t *testing.T) {
	t.Run("pre violated: has parent", func(t *testing.T) {
		mustPanic(t, func() {

		})
	})
	t.Run("find target [skipped]", func(t *testing.T) {
		t.Run("merge up [skipped]", func(t *testing.T) {
			t.Run("→ return:not found", func(t *testing.T) {
			})
		})
		t.Run("merge up", func(t *testing.T) {
			t.Run("no overflow", func(t *testing.T) {
				t.Run("→ return:not found", func(t *testing.T) {
				})
			})
		})
	})
	t.Run("find target", func(t *testing.T) {
		t.Run("target has key", func(t *testing.T) {
			t.Run("→ return:found", func(t *testing.T) {
			})
		})
		t.Run("is leaf", func(t *testing.T) {
			t.Run("merge up [skipped]", func(t *testing.T) {
				t.Run("→ return:not found", func(t *testing.T) {
				})
			})
			t.Run("merge up", func(t *testing.T) {
				t.Run("no overflow", func(t *testing.T) {
					t.Run("→ return:not found", func(t *testing.T) {
					})
				})
			})
		})
	})
}

func mustPanic(t *testing.T, f func()) any {
	t.Helper()
	var recovered any
	func() {
		defer func() {
			recovered = recover()
		}()
		f()
	}()
	if recovered == nil {
		t.Fatal("expected panic")
	}
	return recovered
}
