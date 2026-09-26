package main

import (
	"math"
	"slices"
	"sync"
	"time"
)

// Store keeps hives in memory.
type Store struct {
	mu     sync.Mutex
	hives  []Hive
	nextID int64
}

// NewStore returns a store with a few hives.
func NewStore() *Store {
	s := &Store{nextID: 1}
	created := time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC)
	for _, h := range []Hive{
		{Name: "Linden", Status: StatusActive, Bees: 42000, HoneyKg: 12.5, Queen: &Queen{Breed: "carniolan", BornOn: "2025-05-14"}, Location: Location{55.75, 37.62}, Tags: []string{"meadow"}},
		{Name: "Clover", Status: StatusSwarming, Bees: 55000, HoneyKg: 19, Queen: &Queen{Breed: "buckfast", BornOn: "2024-06-02"}, Location: Location{55.76, 37.64}, Tags: []string{"meadow", "calm"}},
		{Name: "Heather", Status: StatusEmpty, Location: Location{55.74, 37.60}, Tags: []string{}},
	} {
		h.CreatedAt = created
		s.insert(h)
	}
	return s
}

func (s *Store) insert(h Hive) Hive {
	h.ID = s.nextID
	s.nextID++
	s.hives = append(s.hives, h)
	return h
}

func (s *Store) index(id int64) int {
	return slices.IndexFunc(s.hives, func(h Hive) bool { return h.ID == id })
}

// List returns a page of hives that match the filters.
func (s *Store) List(status HiveStatus, tags []string, limit, offset int) ([]Hive, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var match []Hive
	for _, h := range s.hives {
		if status != "" && h.Status != status {
			continue
		}
		if slices.ContainsFunc(tags, func(t string) bool { return !slices.Contains(h.Tags, t) }) {
			continue
		}
		match = append(match, h)
	}
	total := len(match)
	start := min(offset, total)
	end := min(start+limit, total)
	return slices.Clone(match[start:end]), total
}

// Create adds a hive.
func (s *Store) Create(n NewHive) Hive {
	s.mu.Lock()
	defer s.mu.Unlock()
	tags := n.Tags
	if tags == nil {
		tags = []string{}
	}
	return s.insert(Hive{Name: n.Name, Status: n.Status, Location: n.Location, Tags: tags, CreatedAt: time.Now().UTC()})
}

// Get returns a hive.
func (s *Store) Get(id int64) (Hive, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := s.index(id); i >= 0 {
		return s.hives[i], true
	}
	return Hive{}, false
}

// Update applies the set fields of u.
func (s *Store) Update(id int64, u HiveUpdate) (Hive, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.index(id)
	if i < 0 {
		return Hive{}, false
	}
	h := &s.hives[i]
	if u.Name != nil {
		h.Name = *u.Name
	}
	if u.Status != nil {
		h.Status = *u.Status
	}
	if u.Queen != nil {
		h.Queen = u.Queen
	}
	return *h, true
}

// Delete removes a hive.
func (s *Store) Delete(id int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.index(id)
	if i < 0 {
		return false
	}
	s.hives = slices.Delete(s.hives, i, i+1)
	return true
}

// AddBees moves bees into a hive.
func (s *Store) AddBees(id int64, count int) (Hive, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.index(id)
	if i < 0 {
		return Hive{}, false
	}
	h := &s.hives[i]
	h.Bees += count
	if h.Status == StatusEmpty {
		h.Status = StatusActive
	}
	return *h, true
}

// reserveKg is the honey bees keep for themselves.
const reserveKg = 2

// Harvest takes kg of honey and returns what is left.
func (s *Store) Harvest(id int64, kg float64) (float64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.index(id)
	if i < 0 {
		return 0, errHiveNotFound(id)
	}
	h := &s.hives[i]
	available := math.Max(0, h.HoneyKg-reserveKg)
	if kg > available {
		return 0, errNotEnoughHoney(available)
	}
	h.HoneyKg = math.Round((h.HoneyKg-kg)*10) / 10
	return h.HoneyKg, nil
}

// Stats counts hives, bees and honey.
func (s *Store) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := Stats{Hives: len(s.hives)}
	for _, h := range s.hives {
		st.Bees += h.Bees
		st.HoneyKg += h.HoneyKg
	}
	return st
}
