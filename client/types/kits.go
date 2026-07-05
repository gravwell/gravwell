/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"errors"
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"time"
	"unicode"
)

const (
	kitIdBase    string = `io.gravwell.user.`
	kitIdRandLen int    = 8
)

var (
	ErrInvalidType = errors.New("invalid KitItem Type")
	ErrInvalidName = errors.New("invalid KitItem Name")
	ErrInvalidId   = errors.New("invalid KitItem ID")
)

type KitConfigMacro struct {
	MacroName     string // The name of the macro which will be created
	Description   string // a verbose description of what this *does*
	DefaultValue  string // Should be defined at kit creation time
	Value         string // Set by the UI when preparing for installation
	Type          string // "TAG" or "OTHER"
	InstalledByID string // if the macro already exists, the ID of the kit that installed it
}

// KitConfig represents rules, labels, and other configuration options used
// during kit installation.
type KitConfig struct {
	OverwriteExisting     bool `json:",omitempty"`
	AllowUnsigned         bool `json:",omitempty"`
	InstallationReaders   ACL
	InstallationWriters   ACL
	Labels                []string `json:",omitempty"` // labels applied to each *item*
	KitLabels             []string `json:",omitempty"` // labels applied to the *kit* itself
	ConfigMacros          []KitConfigMacro
	AutomationDeployRules map[string]AutomationDeployConfig // overrides for defaults
}

// KitItem implements the generic container for each item in a kit (dashboard, query, etc)
type KitItem struct {
	Name        string // User-friendly
	Description string
	Type        KitAssetType
	ID          string // Unique
	Hash        string

	// The fields below may be set depending on the type.
	DefaultDeploymentRules AutomationDeployConfig // set by automations (scripts, scheduled searches, flows)
	Tags                   []string               `json:",omitempty"` // set by AXes
}

func (ki KitItem) Validate() error {
	if ki.Name == "" {
		return ErrInvalidName
	} else if ki.ID == "" {
		return ErrInvalidId
	}
	if !ki.Type.Valid() {
		return ErrInvalidType
	}
	return nil
}

func (ki KitItem) String() (r string) {
	r = fmt.Sprintf("%s %s %s", ki.ID, ki.Type, ki.Name)
	if v := ki.Description; len(v) != 0 {
		r += ` ` + v
	}
	return
}

// Filename returns a suitable filename for the item.
func (ki KitItem) Filename() string {
	return ki.Name + `.` + string(ki.Type)
}

// ModifiedKitItem is wraps a KitItem with additional information regarding the
// kit's version and origin.
type ModifiedKitItem struct {
	KitItem
	KitID      string
	KitVersion int
	KitName    string
	Diff       map[string]KitModifiedContents
}

// KitModifiedContents shows the original (kit-installed) and current
// (presumably user-modified) values of a single field in a kit asset
// which has been detected as modified.
type KitModifiedContents struct {
	Original any
	Modified any
}

// KitState is the data type that is actually stored in the registry
type KitState struct {
	CommonFields

	KitFileHash string // The hash of the kit file itself, which we store for later reference

	KitID                string // e.g. "io.gravwell.foo"
	KitVersion           int
	Readme               string
	Icon                 string //use for icon when in the context of a kit
	Banner               string //use for banner in a kit
	Cover                string //use for cover image on a kit
	Signed               bool
	MinVersion           CanonicalVersion
	MaxVersion           CanonicalVersion
	Items                []KitItem
	RequiredDependencies []KitMetadata
	ConfigMacros         []KitConfigMacro

	// These fields are only set once the kit is actually installed, not just staged
	Installed           bool             //true means everything was pushed in, false means it is JUST staged
	InstallationTime    time.Time        // the time at which this kit was installed
	InstallationVersion CanonicalVersion // the Gravwell version in use when this kit was installed

	// Items below are set during staging and can be ignored if Installed == true
	ModifiedItems    []ModifiedKitItem // Items which were installed by a previous version of the kit and have been modified by the user
	ConflictingItems []KitItem         // items which will overwrite a user-created object
}

type KitStateListResponse struct {
	BaseListResponse
	Results []KitState
}

type KitEmbeddedItem struct {
	KitItem
	Content []byte `json:",omitempty"` //the actual contents of the item e.g. a license
}

// KitBuildRequest is used to request a kit be built
type KitBuildRequest struct {
	CommonFields
	KitID                 string
	Readme                string
	KitVersion            int
	MinVersion            CanonicalVersion
	MaxVersion            CanonicalVersion
	Dashboards            []string          `json:",omitempty"`
	Templates             []string          `json:",omitempty"`
	Actionables           []string          `json:",omitempty"`
	Resources             []string          `json:",omitempty"`
	ScheduledSearches     []string          `json:",omitempty"`
	ScheduledScripts      []string          `json:",omitempty"`
	Flows                 []string          `json:",omitempty"`
	Macros                []string          `json:",omitempty"`
	Extractors            []string          `json:",omitempty"`
	Files                 []string          `json:",omitempty"`
	SavedQueries          []string          `json:",omitempty"`
	Playbooks             []string          `json:",omitempty"`
	Alerts                []string          `json:",omitempty"`
	EmbeddedItems         []KitEmbeddedItem `json:",omitempty"`
	Icon                  string            `json:",omitempty"`
	Banner                string            `json:",omitempty"`
	Cover                 string            `json:",omitempty"`
	Dependencies          []KitDependency   `json:",omitempty"`
	ConfigMacros          []KitConfigMacro
	AutomationDeployRules map[string]AutomationDeployConfig
	BuildDate             time.Time `db:"build_date"`
}

type KitBuildRequestListResponse struct {
	BaseListResponse
	Results []KitBuildRequest
}

type KitBuildResponse struct {
	UUID string
	Size int64
	UID  int32 `json:",omitempty"`
}

func (ps *KitState) UpdateItem(name string, tp KitAssetType, id string) error {
	for i := range ps.Items {
		if ps.Items[i].Name == name && ps.Items[i].Type == tp {
			ps.Items[i].ID = id
			return nil
		}
	}
	return errors.New("not found")
}

func (ps *KitState) AddItem(itm KitItem) error {
	for i := range ps.Items {
		if ps.Items[i].Name == itm.Name && ps.Items[i].Type == itm.Type {
			return errors.New("already exists")
		}
	}
	ps.Items = append(ps.Items, itm)
	return nil
}

func (ps *KitState) GetItem(name string, tp KitAssetType) (KitItem, error) {
	for i := range ps.Items {
		if ps.Items[i].Name == name && ps.Items[i].Type == tp {
			return ps.Items[i], nil
		}
	}
	return KitItem{}, errors.New("not found")
}

func (ps *KitState) RemoveItem(name string, tp KitAssetType) error {
	for i := range ps.Items {
		if ps.Items[i].Name == name && ps.Items[i].Type == tp {
			ps.Items = append(ps.Items[:i], ps.Items[i+1:]...)
			return nil
		}
	}
	return errors.New("not found")
}

func (pbr *KitBuildRequest) validateReferencedFile(val, name string) error {
	if !slices.Contains(pbr.Files, val) {
		return fmt.Errorf("The %s file ID %s is not included in the kit", name, val)
	}
	return nil
}

func (pbr *KitBuildRequest) Validate() error {
	if pbr.KitID = strings.TrimSpace(pbr.KitID); len(pbr.KitID) == 0 {
		return errors.New("empty KitID")
	}
	if !isLetterNumberPeriod(pbr.KitID) {
		return errors.New("invalid KitID")
	}
	if pbr.Name = strings.TrimSpace(pbr.Name); len(pbr.Name) == 0 {
		return errors.New("empty Name")
	}
	if pbr.Version == 0 {
		pbr.Version = 1
	}
	// if it's not set at all, just set it
	if !pbr.MinVersion.Enabled() {
		pbr.MinVersion = CanonicalVersion{Major: 6}
	} else if pbr.MinVersion.Major < 6 {
		// if it's set but too old, error
		return errors.New("MinVersion on newly-built kits must now exceed 6.0.0")
	}
	if pbr.MaxVersion.Enabled() && pbr.MinVersion.Compare(pbr.MaxVersion) < 0 {
		return errors.New("MaxVersion must exceed MinVersion")
	}
	if slices.Contains(pbr.Dashboards, "") {
		return errors.New("empty dashboard ID")
	}
	for i := range pbr.Resources {
		pbr.Resources[i] = strings.TrimSpace(pbr.Resources[i]) //clean it
	}
	if slices.Contains(pbr.ScheduledSearches, "") {
		return fmt.Errorf("empty scheduled search ID")
	}
	if slices.Contains(pbr.ScheduledScripts, "") {
		return fmt.Errorf("empty scheduled script ID")
	}
	if slices.Contains(pbr.Flows, "") {
		return fmt.Errorf("empty flow ID")
	}
	if slices.Contains(pbr.Macros, "") {
		return errors.New("invalid macro ID")
	}
	if slices.Contains(pbr.Templates, "") {
		return errors.New("invalid template ID")
	}
	if slices.Contains(pbr.Actionables, "") {
		return errors.New("invalid actionable ID")
	}
	if slices.Contains(pbr.Files, "") {
		return errors.New("invalid file ID")
	}
	if slices.Contains(pbr.Playbooks, "") {
		return errors.New("empty playbook ID")
	}
	if slices.Contains(pbr.Alerts, "") {
		return errors.New("empty alert ID")
	}

	if pbr.Icon != `` {
		if err := pbr.validateReferencedFile(pbr.Icon, `Icon`); err != nil {
			return err
		}
	}
	if pbr.Banner != `` {
		if err := pbr.validateReferencedFile(pbr.Banner, `Banner`); err != nil {
			return err
		}
	}
	if pbr.Cover != `` {
		if err := pbr.validateReferencedFile(pbr.Cover, `Cover`); err != nil {
			return err
		}
	}
	idMp := map[KitDependency]es{}
	for _, dp := range pbr.Dependencies {
		if _, ok := idMp[dp]; ok {
			return fmt.Errorf("dependency %s %d is duplicated", dp.KitID, dp.MinVersion)
		}
		idMp[dp] = empty
	}

	for _, emb := range pbr.EmbeddedItems {
		if len(emb.Name) == 0 {
			return errors.New("missing name on embedded item")
		} else if len(emb.Type) == 0 {
			return errors.New("embedded item must have a type")
		} else if len(emb.Content) == 0 {
			return errors.New("embedded content items must not be empty")
		}
	}

	kitItemCount := len(pbr.Dashboards) + len(pbr.Templates) + len(pbr.Actionables) + len(pbr.Resources) + len(pbr.ScheduledSearches) + len(pbr.ScheduledScripts) + len(pbr.Flows) + len(pbr.Macros) + len(pbr.Extractors) + len(pbr.Files) + len(pbr.SavedQueries) + len(pbr.Playbooks) + len(pbr.Alerts)
	if kitItemCount == 0 {
		return errors.New("build request does not contain any items")
	}
	return nil
}

func isLetterNumberPeriod(s string) bool {
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '.' {
			return false
		}
	}
	return true
}

func randKitId() string {
	var letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ01234567890")
	b := make([]rune, kitIdRandLen)
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	for i := range b {
		b[i] = letterRunes[r.Intn(len(letterRunes))]
	}
	return kitIdBase + string(b)
}

// KitDependency declares a series of kits and minimum version requirements
type KitDependency struct {
	KitID      string
	MinVersion int
}

// KitMetadata is a struct that is primarily served by the
// kit server, we use this to record info about a kit so the GUI
// and hint to users what kits they shoudld install.
type KitMetadata struct {
	KitID         string // e.g. "io.gravwell.foo"
	Name          string
	Description   string
	ID            string // Identifies a specific build of the kit, makes it easy to download
	Version       int
	Readme        string
	Signed        bool
	AdminRequired bool
	MinVersion    CanonicalVersion
	MaxVersion    CanonicalVersion
	Size          int64
	Created       time.Time
	Ingesters     []string //ingesters associated with the kit
	Tags          []string //tags associated with the kit
	Assets        []KitMetadataAsset
	Dependencies  []KitDependency
	Items         []KitItem
	ConfigMacros  []KitConfigMacro
}

// KitMetadataAsset stores items that might be associated with kits when hosting them
// we use these to enable pinning additional stuff to a kit.
type KitMetadataAsset struct {
	Type     string
	Source   string //URL
	Legend   string //some description about the asset
	Featured bool   //should be an image, will be used for cover image
	Banner   bool   //should be an image, will be used for upper banner image
}

func (kma KitMetadataAsset) String() (s string) {
	if kma.Featured {
		s = `* `
	}
	s += fmt.Sprintf("%s (%s) %s", kma.Type, kma.Source, kma.Legend)
	return
}

type InstallStatus struct {
	Owner       int32
	Done        bool
	itemCount   int
	itemsDone   int
	Percentage  float64
	CurrentStep string
	Error       string
	Log         string
	InstallID   int32
	Updated     time.Time
}

func NewInstallStatus(itemcount int, installID int32, uid int32) *InstallStatus {
	return &InstallStatus{itemCount: itemcount, Updated: time.Now(), InstallID: installID, Owner: uid}
}

func (i *InstallStatus) SetDone() {
	i.Updated = time.Now()
	i.Done = true
}

func (i *InstallStatus) ItemDone() {
	i.Updated = time.Now()
	if i.itemsDone < i.itemCount {
		i.itemsDone++
	}
	i.Percentage = float64(i.itemsDone) / float64(i.itemCount)
}

func (i *InstallStatus) UpdateCurrentStep(step string) {
	i.Updated = time.Now()
	i.CurrentStep = step
	i.Log = fmt.Sprintf("%v\n%v", i.Log, step)
}

func (i *InstallStatus) SetError(err error) {
	i.Updated = time.Now()
	i.Log = fmt.Sprintf("%v\n%v", i.Log, err)
	i.Error = err.Error()
	i.Done = true
}

type KitItemStatus struct {
	Item  KitItem
	Error string
}

type KitModifyReport struct {
	Statuses []KitItemStatus
	WasError bool
}
