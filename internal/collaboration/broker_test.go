package collaboration

import (
	"sync"
	"testing"
	"time"
)

func TestBrokerPublishSubscribe(t *testing.T) {
	b := NewBroker()
	ch, v0 := b.Subscribe("v1")
	if v0 != 0 {
		t.Fatalf("initial version = %d", v0)
	}
	defer b.Unsubscribe("v1", ch)

	b.Publish(Event{VaultID: "v1", FileID: 1, Kind: "changed"})
	select {
	case ev := <-ch:
		if ev.Kind != "changed" || ev.Revision != 1 {
			t.Fatalf("event = %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no event delivered")
	}
	if v := b.CurrentVersion("v1"); v != 1 {
		t.Fatalf("version = %d", v)
	}
}

func TestBrokerIsolationByVault(t *testing.T) {
	b := NewBroker()
	chA, _ := b.Subscribe("a")
	chB, _ := b.Subscribe("b")
	defer b.Unsubscribe("a", chA)
	defer b.Unsubscribe("b", chB)

	b.Publish(Event{VaultID: "a", Kind: "changed"})
	select {
	case <-chA:
	default:
		t.Fatal("vault a subscriber missed event")
	}
	select {
	case <-chB:
		t.Fatal("vault b subscriber received vault a event")
	default:
	}
}

func TestBrokerPublishToDeliversEventOnSeparateTopic(t *testing.T) {
	// Given
	b := NewBroker()
	ch, _ := b.Subscribe("user:7")
	defer b.Unsubscribe("user:7", ch)
	event := Event{VaultID: "owner-vault", Kind: "changed"}

	// When
	b.PublishTo("user:7", event)

	// Then
	select {
	case got := <-ch:
		if got.VaultID != event.VaultID || got.Revision != 1 {
			t.Fatalf("event = %+v", got)
		}
	default:
		t.Fatal("account subscriber missed event")
	}
}

func TestBrokerWaitVersion(t *testing.T) {
	b := NewBroker()
	b.Publish(Event{VaultID: "w", Kind: "changed"})
	// version 已经是 1，last=0 应立即返回。
	v, changed := b.WaitVersion("w", 0, 2*time.Second)
	if !changed || v != 1 {
		t.Fatalf("wait version: v=%d changed=%v", v, changed)
	}
}

func TestBrokerWaitVersionTimeout(t *testing.T) {
	b := NewBroker()
	start := time.Now()
	v, changed := b.WaitVersion("t", 5, 200*time.Millisecond)
	if changed {
		t.Fatalf("expected timeout, got changed v=%d", v)
	}
	if time.Since(start) < 150*time.Millisecond {
		t.Fatal("returned too early")
	}
}

func TestBrokerConcurrentPublish(t *testing.T) {
	b := NewBroker()
	ch, _ := b.Subscribe("c")
	defer b.Unsubscribe("c", ch)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			b.Publish(Event{VaultID: "c", FileID: uint(n), Kind: "changed"})
		}(i)
	}
	wg.Wait()
	if v := b.CurrentVersion("c"); v != 50 {
		t.Fatalf("version = %d, want 50", v)
	}
}
