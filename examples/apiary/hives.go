package main

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pluhsoft/swaggerkit"
)

// HiveStatus is the state of a bee colony.
type HiveStatus string

const (
	StatusActive   HiveStatus = "active"
	StatusSwarming HiveStatus = "swarming"
	StatusDormant  HiveStatus = "dormant"
	StatusEmpty    HiveStatus = "empty"
)

// Enum lists the values for validation and documentation.
func (HiveStatus) Enum() []HiveStatus {
	return []HiveStatus{StatusActive, StatusSwarming, StatusDormant, StatusEmpty}
}

// Hive is a beehive in the apiary.
type Hive struct {
	ID        int64      `json:"id" doc:"Hive ID" example:"1"`
	Name      string     `json:"name" doc:"Name painted on the hive" example:"Linden"`
	Status    HiveStatus `json:"status" doc:"Colony state"`
	Bees      int        `json:"bees" doc:"Number of bees living in the hive" example:"42000"`
	HoneyKg   float64    `json:"honeyKg" doc:"Honey stored in the hive, kg" example:"12.5"`
	Queen     *Queen     `json:"queen,omitempty" doc:"Queen of the colony, if there is one"`
	Location  Location   `json:"location" doc:"Where the hive stands"`
	Tags      []string   `json:"tags" doc:"Free-form labels" example:"[\"meadow\",\"calm\"]"`
	CreatedAt time.Time  `json:"createdAt" doc:"When the hive was set up"`
}

// Queen is the queen bee of a colony.
type Queen struct {
	Breed  string `json:"breed" doc:"Bee breed" validate:"oneof=carniolan italian buckfast" example:"carniolan"`
	BornOn string `json:"bornOn" doc:"Date the queen hatched" validate:"date" example:"2026-05-14"`
}

// Location is a point on the map.
type Location struct {
	Lat float64 `json:"lat" doc:"Latitude" validate:"min=-90,max=90" example:"55.75"`
	Lon float64 `json:"lon" doc:"Longitude" validate:"min=-180,max=180" example:"37.62"`
}

// Page is a page of a list.
type Page[T any] struct {
	Items  []T `json:"items" doc:"Items on this page"`
	Total  int `json:"total" doc:"Number of items in all pages" example:"3"`
	Limit  int `json:"limit" doc:"Page size" example:"20"`
	Offset int `json:"offset" doc:"Items skipped" example:"0"`
}

// Pagination is embedded into list inputs.
type Pagination struct {
	Limit  int `query:"limit" doc:"Page size" validate:"min=1,max=100" default:"20"`
	Offset int `query:"offset" doc:"Items to skip" validate:"min=0" default:"0"`
}

// ListHivesInput filters the hive list.
type ListHivesInput struct {
	Pagination
	Status HiveStatus `query:"status" doc:"Only hives in this state"`
	Tag    []string   `query:"tag" doc:"Only hives with all of these tags" validate:"max=5"`
}

// ListHives returns hives, newest last.
func (a *Apiary) ListHives(ctx context.Context, in ListHivesInput) (Page[Hive], error) {
	items, total := a.store.List(in.Status, in.Tag, in.Limit, in.Offset)
	swaggerkit.ResponseHeader(ctx).Set("X-Total-Count", strconv.Itoa(total))
	return Page[Hive]{Items: items, Total: total, Limit: in.Limit, Offset: in.Offset}, nil
}

// NewHive is the request to set up a hive.
type NewHive struct {
	Name     string     `json:"name" doc:"Name painted on the hive" validate:"required,min=1,max=64" example:"Linden"`
	Status   HiveStatus `json:"status,omitempty" doc:"Initial colony state" default:"empty"`
	Location Location   `json:"location" doc:"Where the hive stands"`
	Tags     []string   `json:"tags,omitempty" doc:"Free-form labels" validate:"max=10,unique,dive,min=1,max=32"`
}

// CreateHive sets up a new hive.
func (a *Apiary) CreateHive(ctx context.Context, in NewHive) (Hive, error) {
	return a.store.Create(in), nil
}

// HivePath selects a hive by its path parameter.
type HivePath struct {
	ID int64 `path:"hiveId" doc:"Hive ID" validate:"min=1" example:"1"`
}

// GetHive returns one hive.
func (a *Apiary) GetHive(ctx context.Context, in HivePath) (Hive, error) {
	h, ok := a.store.Get(in.ID)
	if !ok {
		return Hive{}, errHiveNotFound(in.ID)
	}
	return h, nil
}

// HiveUpdate changes some fields of a hive. Missing fields stay unchanged.
type HiveUpdate struct {
	Name   *string     `json:"name,omitempty" doc:"New name" validate:"min=1,max=64"`
	Status *HiveStatus `json:"status,omitempty" doc:"New colony state"`
	Queen  *Queen      `json:"queen,omitempty" doc:"New queen"`
}

// UpdateHiveInput is the input of UpdateHive.
type UpdateHiveInput struct {
	HivePath
	Body HiveUpdate
}

// UpdateHive changes a hive.
func (a *Apiary) UpdateHive(ctx context.Context, in UpdateHiveInput) (Hive, error) {
	h, ok := a.store.Update(in.ID, in.Body)
	if !ok {
		return Hive{}, errHiveNotFound(in.ID)
	}
	return h, nil
}

// DeleteHive removes a hive.
func (a *Apiary) DeleteHive(ctx context.Context, in HivePath) (swaggerkit.NoContent, error) {
	if !a.store.Delete(in.ID) {
		return swaggerkit.NoContent{}, errHiveNotFound(in.ID)
	}
	return swaggerkit.NoContent{}, nil
}

// AddBeesInput moves bees into a hive.
type AddBeesInput struct {
	HivePath
	Body struct {
		Count int `json:"count" doc:"Number of bees to add" validate:"required,min=1,max=50000" example:"5000"`
	}
}

// AddBees moves bees into a hive. An empty hive becomes active.
func (a *Apiary) AddBees(ctx context.Context, in AddBeesInput) (Hive, error) {
	h, ok := a.store.AddBees(in.ID, in.Body.Count)
	if !ok {
		return Hive{}, errHiveNotFound(in.ID)
	}
	return h, nil
}

// Harvest is honey taken from a hive.
type Harvest struct {
	HiveID      int64     `json:"hiveId" doc:"Hive the honey was taken from" example:"1"`
	Kg          float64   `json:"kg" doc:"Honey taken, kg" example:"4.5"`
	RemainingKg float64   `json:"remainingKg" doc:"Honey left in the hive, kg" example:"8"`
	HarvestedAt time.Time `json:"harvestedAt" doc:"When the honey was taken"`
	Beekeeper   string    `json:"beekeeper" doc:"Who harvested" example:"beekeeper"`
}

// HarvestInput takes honey from a hive.
type HarvestInput struct {
	HivePath
	Body struct {
		Kg float64 `json:"kg" doc:"Honey to take, kg" validate:"required,gt=0,max=100" example:"4.5"`
	}
}

// HarvestHoney takes honey from a hive. Bees need to keep some honey,
// so at most the stored amount minus 2 kg can be taken.
func (a *Apiary) HarvestHoney(ctx context.Context, in HarvestInput) (Harvest, error) {
	remaining, err := a.store.Harvest(in.ID, in.Body.Kg)
	if err != nil {
		return Harvest{}, err
	}
	return Harvest{
		HiveID:      in.ID,
		Kg:          in.Body.Kg,
		RemainingKg: remaining,
		HarvestedAt: time.Now().UTC(),
		Beekeeper:   BeekeeperFrom(ctx),
	}, nil
}

// HiveLabel returns a printable text label for a hive.
func (a *Apiary) HiveLabel(ctx context.Context, in HivePath) (*swaggerkit.File, error) {
	h, ok := a.store.Get(in.ID)
	if !ok {
		return nil, errHiveNotFound(in.ID)
	}
	label := fmt.Sprintf("Hive #%d %s\nStatus: %s\nBees: %d\nHoney: %.1f kg\n", h.ID, h.Name, h.Status, h.Bees, h.HoneyKg)
	return &swaggerkit.File{
		Body:        strings.NewReader(label),
		ContentType: "text/plain; charset=utf-8",
		Name:        fmt.Sprintf("hive-%d.txt", h.ID),
		ModTime:     h.CreatedAt,
	}, nil
}

// Stats summarizes the apiary.
type Stats struct {
	Hives   int     `json:"hives" doc:"Number of hives" example:"3"`
	Bees    int     `json:"bees" doc:"Bees in all hives" example:"97000"`
	HoneyKg float64 `json:"honeyKg" doc:"Honey in all hives, kg" example:"31.5"`
}

// GetStats counts hives, bees and honey.
func (a *Apiary) GetStats(ctx context.Context, _ struct{}) (Stats, error) {
	return a.store.Stats(), nil
}

func errHiveNotFound(id int64) error {
	return swaggerkit.NotFound(fmt.Sprintf("hive %d does not exist", id))
}

// errNotEnoughHoney is returned when a harvest would leave the bees hungry.
func errNotEnoughHoney(available float64) error {
	return swaggerkit.NewError(http.StatusConflict, fmt.Sprintf("only %.1f kg can be taken; bees keep 2 kg for themselves", available))
}
