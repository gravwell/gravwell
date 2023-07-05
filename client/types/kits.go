/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"
	"unicode"

	"github.com/buger/jsonparser"
	"github.com/google/uuid"
)

const (
	kitIdBase    string = `io.gravwell.user.`
	kitIdRandLen int    = 8
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
	OverwriteExisting     bool  `json:",omitempty"`
	Global                bool  `json:",omitempty"`
	AllowExternalResource bool  `json:",omitempty"`
	AllowUnsigned         bool  `json:",omitempty"`
	InstallationGroup     int32 `json:",omitempty"` // deprecated, use InstallationGroups instead
	InstallationGroups    []int32
	Labels                []string `json:",omitempty"` // labels applied to each *item*
	KitLabels             []string `json:",omitempty"` // labels applied to the *kit* itself
	ConfigMacros          []KitConfigMacro
	ScriptDeployRules     map[string]ScriptDeployConfig // overrides for defaults
}

// Each item in a kit (dashboard, query, etc) is represented by a KitItem
// object.
type KitItem struct {
	Name           string
	Type           string
	ID             string          `json:",omitempty"` //the UUID
	AdditionalInfo json.RawMessage `json:",omitempty"`
	Hash           [sha256.Size]byte
}

// SourcedKitItem is wraps a KitItem with additional information regarding the
// kit's version and origin.
type SourcedKitItem struct {
	KitItem
	KitID      string
	KitVersion uint
	KitName    string
}

// The kit data type that is actually stored in the datastore
type KitState struct {
	ID                   string
	Name                 string
	Description          string
	Readme               string
	UUID                 string
	Signed               bool
	AdminRequired        bool
	MinVersion           CanonicalVersion `json:",omitempty"`
	MaxVersion           CanonicalVersion `json:",omitempty"`
	UID                  int32            `json:",omitempty"`
	Version              uint
	Items                []KitItem
	Labels               []string
	Icon                 string           //use for icon when in the context of a kit
	Banner               string           //use for banner in a kit
	Cover                string           //use for cover image on a kit
	ModifiedItems        []SourcedKitItem // Items which were installed by a previous version of the kit and have been modified by the user
	ConflictingItems     []KitItem        // items which will overwrite a user-created object
	RequiredDependencies []KitMetadata
	Installed            bool      //true means everything was pushed in, false means it is JUST staged
	InstallationTime     time.Time // the time at which this kit was installed
	ConfigMacros         []KitConfigMacro
	Metadata             json.RawMessage `json:",omitempty"`
}

// the type that handles the datastore system
type KitManifest struct {
	UID         int32
	GIDs        []int32
	Global      bool
	UUID        uuid.UUID
	Data        []byte
	WebserverID uuid.UUID // which webserver created this manifest: needed to manage staged manifests & on-disk kit files
	Synced      bool
}

// type that is used when sending back lists via a ADMIN request (show uid and gid)
type IdKitState struct {
	UUID uuid.UUID
	UID  int32
	GIDs []int32
	KitState
}

type KitEmbeddedItem struct {
	KitItem
	Content []byte `json:",omitempty"` //the actual contents of the kit
}

// the type that is used to request a kit be built
type KitBuildRequest struct {
	ID                string
	Name              string
	Description       string
	Readme            string
	Version           uint
	MinVersion        CanonicalVersion  `json:",omitempty"`
	MaxVersion        CanonicalVersion  `json:",omitempty"`
	Dashboards        []uint64          `json:",omitempty"`
	Templates         []uuid.UUID       `json:",omitempty"`
	Pivots            []uuid.UUID       `json:",omitempty"`
	Resources         []string          `json:",omitempty"`
	ScheduledSearches []int32           `json:",omitempty"`
	Flows             []int32           `json:",omitempty"`
	Macros            []uint64          `json:",omitempty"`
	Extractors        []uuid.UUID       `json:",omitempty"`
	Files             []uuid.UUID       `json:",omitempty"`
	SearchLibraries   []uuid.UUID       `json:",omitempty"`
	Playbooks         []uuid.UUID       `json:",omitempty"`
	EmbeddedItems     []KitEmbeddedItem `json:",omitempty"`
	Icon              string            `json:",omitempty"`
	Banner            string            `json:",omitempty"`
	Cover             string            `json:",omitempty"`
	Dependencies      []KitDependency   `json:",omitempty"`
	ConfigMacros      []KitConfigMacro
	ScriptDeployRules map[int32]ScriptDeployConfig
}

// this is what we store in the datastore
type StoredBuildRequest struct {
	UID int32
	KitBuildRequest
	BuildDate time.Time
}

type KitBuildResponse struct {
	UUID string
	Size int64
	UID  int32 `json:",omitempty"`
}

func (pm *KitManifest) Encode(v interface{}) (err error) {
	pm.Data, err = json.Marshal(v)
	return
}

func (pm *KitManifest) Decode(v interface{}) (err error) {
	err = json.Unmarshal(pm.Data, v)
	return
}

func (ps *KitState) UpdateItem(name, tp, id string) error {
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

func (ps *KitState) GetItem(name, tp string) (KitItem, error) {
	for i := range ps.Items {
		if ps.Items[i].Name == name && ps.Items[i].Type == tp {
			return ps.Items[i], nil
		}
	}
	return KitItem{}, errors.New("not found")
}

func (ps *KitState) RemoveItem(name, tp string) error {
	for i := range ps.Items {
		if ps.Items[i].Name == name && ps.Items[i].Type == tp {
			ps.Items = append(ps.Items[:i], ps.Items[i+1:]...)
			return nil
		}
	}
	return errors.New("not found")
}

func (pbr *KitBuildRequest) validateReferencedFile(val, name string) error {
	guid, err := uuid.Parse(val)
	if err != nil {
		return fmt.Errorf("Invalid %s ID: %v", name, err)
	}
	//iterate through the files and make sure the file exists
	var ok bool
	for _, v := range pbr.Files {
		if v == guid {
			ok = true
			break
		}
	}
	if !ok {
		return fmt.Errorf("The %s file ID %s is not included in the kit", name, guid)
	}
	return nil
}

func (pbr *KitBuildRequest) Validate() error {
	if pbr.ID = strings.TrimSpace(pbr.ID); len(pbr.ID) == 0 {
		pbr.ID = randKitId()
	}
	if !isLetterNumberPeriod(pbr.ID) {
		return errors.New("Invalid ID")
	}
	if pbr.Name = strings.TrimSpace(pbr.Name); len(pbr.Name) == 0 {
		return errors.New("empty Name")
	}
	if pbr.Version == 0 {
		pbr.Version = 1
	}
	for i := range pbr.Dashboards {
		if pbr.Dashboards[i] == 0 {
			return errors.New("zero value dashboard id")
		}
	}
	for i := range pbr.Resources {
		pbr.Resources[i] = strings.TrimSpace(pbr.Resources[i]) //clean it
		//attempt to parse it
		if _, err := uuid.Parse(pbr.Resources[i]); err != nil {
			return err
		}
	}
	for i := range pbr.ScheduledSearches {
		if pbr.ScheduledSearches[i] <= 0 {
			return fmt.Errorf("Invalid scheduled search/script ID %d", pbr.ScheduledSearches[i])
		}
	}
	for i := range pbr.Flows {
		if pbr.Flows[i] <= 0 {
			return fmt.Errorf("Invalid flow ID %d", pbr.Flows[i])
		}
	}
	for i := range pbr.Macros {
		if pbr.Macros[i] == 0 {
			return errors.New("Invalid macro ID")
		}
	}
	for i := range pbr.Templates {
		if pbr.Templates[i] == uuid.Nil {
			return errors.New("Zero UUID in templates list")
		}
	}
	for i := range pbr.Pivots {
		if pbr.Pivots[i] == uuid.Nil {
			return errors.New("Zero UUID in pivots list")
		}
	}
	for i := range pbr.Files {
		if pbr.Files[i] == uuid.Nil {
			return errors.New("Zero UUID in file list")
		}
	}
	for i := range pbr.Playbooks {
		if pbr.Playbooks[i] == uuid.Nil {
			return errors.New("Zero UUID in playbook list")
		}
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
			return fmt.Errorf("Dependency %s %d is duplicated", dp.ID, dp.MinVersion)
		}
		idMp[dp] = empty
	}

	for _, emb := range pbr.EmbeddedItems {
		if len(emb.Name) == 0 {
			return errors.New("Missing name on embedded item")
		} else if len(emb.Type) == 0 {
			return errors.New("Embedded item must have a type")
		} else if len(emb.Content) == 0 {
			return errors.New("Embedded content items must not be empty")
		}
	}

	kitItemCount := len(pbr.Dashboards) + len(pbr.Templates) + len(pbr.Pivots) + len(pbr.Resources) + len(pbr.ScheduledSearches) + len(pbr.Flows) + len(pbr.Macros) + len(pbr.Extractors) + len(pbr.Files) + len(pbr.SearchLibraries) + len(pbr.Playbooks)
	if kitItemCount == 0 {
		return errors.New("Build request does not contain any items")
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

func (ki *KitItem) DescriptionString() (s string) {
	//check if there is a Desc fild in the raw json
	var err error
	if s, err = jsonparser.GetString([]byte(ki.AdditionalInfo), `Description`); err != nil {
		s = ``
	}
	return
}

func (ki KitItem) String() (r string) {
	r = fmt.Sprintf("%s %s %s", ki.ID, ki.Type, ki.Name)
	if v := ki.DescriptionString(); len(v) != 0 {
		r += ` ` + v
	}
	return
}

// KitDependency declares a series of kits and minimum version requirements
type KitDependency struct {
	ID         string
	MinVersion uint
}

// KitMetadata is a struct that is primarily served by the
// kit server, we use this to record info about a kit so the GUI
// and hint to users what kits they shoudld install.
type KitMetadata struct {
	ID            string
	Name          string
	GUID          string `json:",omitempty"` // DEPRECATED, TODO: remove
	UUID          string
	Version       uint
	Description   string
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

// KitMetadataAssets are items that might be associated with kits when hosting them
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
