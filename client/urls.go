/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import (
	"fmt"
	"path"

	"github.com/google/uuid"
)

const (
	// login field names
	USER_FIELD string = "User"
	PASS_FIELD string = "Pass"

	// API paths
	LOGIN_URL                        = `/api/login`
	LOGOUT_URL                       = `/api/logout`
	TEMP_TOKEN_URL                   = `/api/login/tmptoken`
	REFRESH_TOKEN_URL                = `/api/login/refreshtoken`
	USER_INFO_URL                    = `/api/info/whoami`
	DESC_URL                         = `/api/stats/sysDesc`
	STATE_URL                        = `/api/stats/ping`
	STATS_URL                        = `/api/stats/sysStats`
	IDX_URL                          = `/api/stats/idxStats`
	INGESTER_URL                     = `/api/stats/igstStats`
	ADD_USER_URL                     = `/api/users`
	USERS_LIST_URL                   = `/api/users`
	USERS_INFO_URL                   = `/api/users/%d`
	USERS_LOCK_URL                   = `/api/users/%d/lock`
	USERS_LOCKED_URL                 = `/api/users/%d/locked`
	USERS_DASHBOARD_URL              = `/api/users/%d/dashboards`
	USERS_MACROS_URL                 = `/api/users/%d/macros`
	USERS_PREFS_URL                  = `/api/users/%d/preferences`
	USERS_ALL_PREFS_URL              = `/api/users/preferences`
	USERS_ADMIN_URL                  = `/api/users/%d/admin`
	USERS_ADMIN_SU_PATH              = `/api/users/su/%d`
	USER_SESSIONS_URL                = `/api/users/%d/sessions`
	CHANGE_PASS_URL                  = `/api/users/%d/pwd`
	USERS_GROUP_URL                  = `/api/users/%d/group`
	USERS_SEARCH_GROUP_URL           = `/api/users/%d/searchgroup`
	WS_STAT_URL                      = `/api/ws/stats`
	WS_SEARCH_URL                    = `/api/ws/search`
	WS_ATTACH_URL                    = `/api/ws/attach/%s`
	API_VERSION_URL                  = `/api/version`
	GROUP_ID_URL                     = `/api/groups/%d`
	GROUP_MEMBERS_URL                = `/api/groups/%d/members`
	GROUP_DASHBOARD_URL              = `/api/groups/%d/dashboards`
	GROUP_MACROS_URL                 = `/api/groups/%d/macros`
	GROUP_URL                        = `/api/groups`
	SEARCH_CTRL_LIST_URL             = `/api/searchctrl`
	SEARCH_CTRL_LIST_ALL_URL         = `/api/searchctrl/all`
	SEARCH_CTRL_URL                  = `/api/searchctrl/%s`
	SEARCH_CTRL_DETAILS              = `/api/searchctrl/%s/details`
	SEARCH_CTRL_BACKGROUND_URL       = `/api/searchctrl/%s/background`
	SEARCH_CTRL_GROUP_URL            = `/api/searchctrl/%s/group`
	SEARCH_CTRL_SAVE_URL             = `/api/searchctrl/%s/save`
	SEARCH_CTRL_STOP_URL             = `/api/searchctrl/%s/stop`
	SEARCH_CTRL_DOWNLOAD_URL         = `/api/searchctrl/%s/download/%s`
	SEARCH_CTRL_IMPORT_URL           = `/api/searchctrl/import`
	SEARCH_HISTORY_URL               = `/api/searchhistory/%s/%d`
	NOTIFICATIONS_URL                = `/api/notifications/`
	NOTIFICATIONS_ID_URL             = `/api/notifications/%d`
	NOTIFICATIONS_SELF_TARGETED_URL  = `/api/notifications/targeted/self`
	LOGGING_PATH_URL                 = `/api/logging`
	TEST_URL                         = `/api/test`
	TEST_AUTH_URL                    = `/api/testauth`
	DASHBOARD_URL                    = `/api/dashboards/%v`
	DASHBOARD_MY_URL                 = `/api/dashboards`
	DASHBOARD_ALL_URL                = `/api/dashboards/all`
	DASHBOARD_CLONE_URL              = `/api/dashboards/%d/clone`
	MACROS_URL                       = `/api/macros`
	MACROS_ALL_URL                   = `/api/macros/all`
	MACROS_ID_URL                    = `/api/macros/%d`
	LICENSE_INFO_URL                 = `/api/license`
	LICENSE_SKU_URL                  = `/api/license/sku`
	LICENSE_SERIAL_URL               = `/api/license/serial`
	LICENSE_UPDATE_URL               = `/api/license/update`
	RESOURCES_LIST_URL               = "/api/resources"
	RESOURCES_GUID_URL               = "/api/resources/%s"
	RESOURCES_GUID_RAW_URL           = "/api/resources/%s/raw"
	RESOURCES_GUID_CLONE_URL         = "/api/resources/%s/clone"
	RESOURCES_LOOKUP_URL             = "/api/resources/lookup/%s"
	SCHEDULED_SEARCH_URL             = "/api/scheduledsearches"
	SCHEDULED_SEARCH_ALL_URL         = "/api/scheduledsearches/all"
	SCHEDULED_SEARCH_ID_URL          = "/api/scheduledsearches/%d"
	SCHEDULED_SEARCH_RESULTS_ID_URL  = "/api/scheduledsearches/%d/results"
	SCHEDULED_SEARCH_ERROR_ID_URL    = "/api/scheduledsearches/%d/error"
	SCHEDULED_SEARCH_STATE_ID_URL    = "/api/scheduledsearches/%d/state"
	SCHEDULED_SEARCH_USER_URL        = "/api/scheduledsearches/user/%d"
	SCHEDULED_SEARCH_CHECKIN_URL     = "/api/scheduledsearches/checkin"
	SCHEDULED_SEARCH_PARSE           = "/api/scheduledsearches/parse"
	FLOW_URL                         = "/api/flows"
	FLOW_ID_URL                      = "/api/flows/%d"
	FLOW_RESULTS_ID_URL              = "/api/flows/%d/results"
	FLOW_ERROR_ID_URL                = "/api/flows/%d/error"
	FLOW_STATE_ID_URL                = "/api/flows/%d/state"
	FLOW_USER_URL                    = "/api/flows/user/%d"
	FLOW_PARSE_URL                   = "/api/flows/parse"
	MAIL_URL                         = "/api/mail"
	MAIL_CONFIGURE_URL               = `/api/mail/configure`
	JSON_INGEST_URL                  = "/api/ingest/json"
	LINES_INGEST_URL                 = "/api/ingest/lines"
	INTERNAL_INGEST_URL              = "/api/ingest/internal"
	TAGS_URL                         = "/api/tags"
	INDEXER_MANAGE_ADD_URL           = "/api/indexer/manage/add"
	INDEXER_INFO_URL                 = "/api/indexer/info"
	KIT_URL                          = `/api/kits`
	KIT_ID_URL                       = `/api/kits/%s`
	KIT_BUILD_URL                    = `/api/kits/build`
	KIT_BUILD_ID_URL                 = `/api/kits/build/%s`
	KIT_STATUS_URL                   = `/api/kits/status`
	KIT_STATUS_ID_URL                = `/api/kits/status/%s`
	KIT_REMOTE_LIST_URL              = `/api/kits/remote/list`
	KIT_REMOTE_LIST_ALL_URL          = `/api/kits/remote/list/all`
	KIT_BUILD_HISTORY_URL            = `/api/kits/build/history`
	KIT_BUILD_HISTORY_ID_URL         = `/api/kits/build/history/%s`
	EXTRACTORS_URL                   = `/api/autoextractors`
	EXTRACTORS_UPLOAD_URL            = `/api/autoextractors/upload`
	EXTRACTORS_TEST_URL              = `/api/autoextractors/test`
	EXTRACTORS_ID_URL                = `/api/autoextractors/%s`
	EXTRACTORS_SYNC_URL              = `/api/autoextractors/sync`
	EXTRACTORS_ENGINES_URL           = `/api/autoextractors/engines`
	EXPLORE_GENERATE_URL             = `/api/explore/generate`
	TEMPLATES_URL                    = "/api/templates"
	TEMPLATES_ID_URL                 = "/api/templates/%s"
	TEMPLATES_ID_DETAILS_URL         = "/api/templates/%s/details"
	PIVOTS_URL                       = "/api/pivots"
	PIVOTS_ID_URL                    = "/api/pivots/%s"
	PIVOTS_ID_DETAILS_URL            = "/api/pivots/%s/details"
	USER_FILES_URL                   = "/api/files"
	USER_FILES_ID_URL                = "/api/files/%s"
	LIBRARY_URL                      = "/api/library"
	LIBRARY_ID_URL                   = "/api/library/%s"
	LIBS_URL                         = `/api/libs`
	CAPABILITY_LIST_URL              = `/api/info/capabilities`
	CAPABILITY_TEMPLATE_LIST_URL     = `/api/info/capabilities/templates`
	CAPABILITY_CURRENT_USER_LIST_URL = `/api/info/capabilities/my`
	CAPABILITY_USER_URL              = `/api/users/%d/capabilities`
	CAPABILITY_GROUP_URL             = `/api/groups/%d/capabilities`
	GROUP_TAG_ACCESS_URL             = `/api/groups/%d/tags`
	USER_TAG_ACCESS_URL              = `/api/users/%d/tags`
	PLAYBOOKS_URL                    = `/api/playbooks`
	PLAYBOOKS_ID_URL                 = `/api/playbooks/%s`
	BACKUP_URL                       = `/api/backup`
	DEPLOYMENT_URL                   = `/api/deployment`
	TOKENS_URL                       = `/api/tokens`
	TOKENS_ID_URL                    = `/api/tokens/%s`
	TOKENS_CAPABILITIES_URL          = `/api/tokens/capabilities`

	// Special APIs for installing licenses
	LICENSE_INIT_UPLOAD = `/license`
	LICENSE_INIT_STATUS = `/license/status`
)

func lockUrl(id int32) string {
	return fmt.Sprintf(USERS_LOCK_URL, id)
}

func lockedUrl(id int32) string {
	return fmt.Sprintf(USERS_LOCKED_URL, id)
}

func usersAdminUrl(id int32) string {
	return fmt.Sprintf(USERS_ADMIN_URL, id)
}

func usersAdminImpersonate(id int32) string {
	return fmt.Sprintf(USERS_ADMIN_SU_PATH, id)
}

func usersInfoUrl(id int32) string {
	return fmt.Sprintf(USERS_INFO_URL, id)
}

func usersChangePassUrl(id int32) string {
	return fmt.Sprintf(CHANGE_PASS_URL, id)
}

func usersGroupUrl(uid int32) string {
	return fmt.Sprintf(USERS_GROUP_URL, uid)
}

func usersSearchGroupUrl(uid int32) string {
	return fmt.Sprintf(USERS_SEARCH_GROUP_URL, uid)
}

func searchHistoryUrl(action string, id int32) string {
	return fmt.Sprintf(SEARCH_HISTORY_URL, action, id)
}

func groupUrl() string {
	return GROUP_URL
}

func groupIdUrl(gid int32) string {
	return fmt.Sprintf(GROUP_ID_URL, gid)
}

func groupMembersUrl(gid int32) string {
	return fmt.Sprintf(GROUP_MEMBERS_URL, gid)
}

func dashboardUrl(id uint64) string {
	return fmt.Sprintf(DASHBOARD_URL, id)
}

func dashboardUrlString(id string) string {
	return fmt.Sprintf(DASHBOARD_URL, id)
}

func cloneDashboardUrl(id uint64) string {
	return fmt.Sprintf(DASHBOARD_CLONE_URL, id)
}

func userDashboardUrl(id int32) string {
	return fmt.Sprintf(USERS_DASHBOARD_URL, id)
}

func groupDashboardUrl(id int32) string {
	return fmt.Sprintf(GROUP_DASHBOARD_URL, id)
}

func allDashboardUrl() string {
	return DASHBOARD_ALL_URL
}

func addDashboardUrl() string {
	return DASHBOARD_MY_URL
}

func myDashboardUrl() string {
	return DASHBOARD_MY_URL
}

func allUsersUrl() string {
	return USERS_LIST_URL
}

func searchCtrlBackgroundUrl(id string) string {
	return fmt.Sprintf(SEARCH_CTRL_BACKGROUND_URL, id)
}

func searchCtrlGroupUrl(id string) string {
	return fmt.Sprintf(SEARCH_CTRL_GROUP_URL, id)
}

func searchCtrlSaveUrl(id string) string {
	return fmt.Sprintf(SEARCH_CTRL_SAVE_URL, id)
}

func searchCtrlDownloadUrl(id, format string) string {
	return fmt.Sprintf(SEARCH_CTRL_DOWNLOAD_URL, id, format)
}

func searchCtrlStopUrl(id string) string {
	return fmt.Sprintf(SEARCH_CTRL_STOP_URL, id)
}

func searchCtrlImportUrl() string {
	return SEARCH_CTRL_IMPORT_URL
}

func searchCtrlIdUrl(id string) string {
	return fmt.Sprintf(SEARCH_CTRL_URL, id)
}

func searchCtrlDetailsUrl(id string) string {
	return fmt.Sprintf(SEARCH_CTRL_DETAILS, id)
}

func sessionsUrl(id int32) string {
	return fmt.Sprintf(USER_SESSIONS_URL, id)
}

func preferencesUrl(id int32) string {
	return fmt.Sprintf(USERS_PREFS_URL, id)
}

func allPreferencesUrl() string {
	return USERS_ALL_PREFS_URL
}

func notificationsUrl(id uint64) string {
	if id == 0 {
		return NOTIFICATIONS_URL
	} else {
		return fmt.Sprintf(NOTIFICATIONS_ID_URL, id)
	}
}

func notificationsSelfTargetedUrl() string {
	return NOTIFICATIONS_SELF_TARGETED_URL
}

func licenseInfoUrl() string {
	return LICENSE_INFO_URL
}

func licenseSKUUrl() string {
	return LICENSE_SKU_URL
}

func licenseSerialUrl() string {
	return LICENSE_SERIAL_URL
}

func licenseUpdateUrl() string {
	return LICENSE_UPDATE_URL
}

func resourcesUrl() string {
	return RESOURCES_LIST_URL
}

func resourcesGuidUrl(guid string) string {
	return fmt.Sprintf(RESOURCES_GUID_URL, guid)
}

func resourcesGuidRawUrl(guid string) string {
	return fmt.Sprintf(RESOURCES_GUID_RAW_URL, guid)
}

func resourcesLookupUrl(name string) string {
	return fmt.Sprintf(RESOURCES_LOOKUP_URL, name)
}

func resourcesCloneUrl(guid string) string {
	return fmt.Sprintf(RESOURCES_GUID_CLONE_URL, guid)
}

func scheduledSearchUrl() string {
	return SCHEDULED_SEARCH_URL
}

func scheduledSearchParseUrl() string {
	return SCHEDULED_SEARCH_PARSE
}

func scheduledSearchAllUrl() string {
	return SCHEDULED_SEARCH_ALL_URL
}

func scheduledSearchIdUrl(id int32) string {
	return fmt.Sprintf(SCHEDULED_SEARCH_ID_URL, id)
}

func scheduledSearchResultsIdUrl(id int32) string {
	return fmt.Sprintf(SCHEDULED_SEARCH_RESULTS_ID_URL, id)
}

func scheduledSearchErrorIdUrl(id int32) string {
	return fmt.Sprintf(SCHEDULED_SEARCH_ERROR_ID_URL, id)
}

func scheduledSearchStateIdUrl(id int32) string {
	return fmt.Sprintf(SCHEDULED_SEARCH_STATE_ID_URL, id)
}

func scheduledSearchUserUrl(uid int32) string {
	return fmt.Sprintf(SCHEDULED_SEARCH_USER_URL, uid)
}

func scheduledSearchCheckinUrl() string {
	return SCHEDULED_SEARCH_CHECKIN_URL
}
func flowUrl() string {
	return FLOW_URL
}

func flowParseUrl() string {
	return FLOW_PARSE_URL
}

func flowIdUrl(id int32) string {
	return fmt.Sprintf(FLOW_ID_URL, id)
}

func flowResultsIdUrl(id int32) string {
	return fmt.Sprintf(FLOW_RESULTS_ID_URL, id)
}

func flowErrorIdUrl(id int32) string {
	return fmt.Sprintf(FLOW_ERROR_ID_URL, id)
}

func flowStateIdUrl(id int32) string {
	return fmt.Sprintf(FLOW_STATE_ID_URL, id)
}

func flowUserUrl(uid int32) string {
	return fmt.Sprintf(FLOW_USER_URL, uid)
}

func loggingUrl() string {
	return LOGGING_PATH_URL
}

func loggingAccessUrl() string {
	return path.Join(LOGGING_PATH_URL, "access")
}

func loggingInfoUrl() string {
	return path.Join(LOGGING_PATH_URL, "info")
}

func loggingWarnUrl() string {
	return path.Join(LOGGING_PATH_URL, "warn")
}

func loggingErrorUrl() string {
	return path.Join(LOGGING_PATH_URL, "error")
}

func addIndexerUrl() string {
	return INDEXER_MANAGE_ADD_URL
}

func wellDataUrl() string {
	return INDEXER_INFO_URL
}

func userMacrosUrl(id int32) string {
	return fmt.Sprintf(USERS_MACROS_URL, id)
}

func groupMacrosUrl(id int32) string {
	return fmt.Sprintf(GROUP_MACROS_URL, id)
}

func macroUrl(id uint64) string {
	return fmt.Sprintf(MACROS_ID_URL, id)
}

func playbookUrl(id uuid.UUID) string {
	return fmt.Sprintf(PLAYBOOKS_ID_URL, id)
}

func kitUrl() string {
	return KIT_URL
}

func remoteKitUrl(all bool) string {
	if all {
		return KIT_REMOTE_LIST_ALL_URL
	}
	return KIT_REMOTE_LIST_URL
}

func kitIdUrl(id string) string {
	return fmt.Sprintf(KIT_ID_URL, id)
}

func kitBuildUrl() string {
	return KIT_BUILD_URL
}

func kitDownloadUrl(id string) string {
	return fmt.Sprintf(KIT_BUILD_ID_URL, id)
}

func kitStatusUrl() string {
	return KIT_STATUS_URL
}

func kitStatusIdUrl(id string) string {
	return fmt.Sprintf(KIT_STATUS_ID_URL, id)
}

func kitBuildHistoryUrl() string {
	return KIT_BUILD_HISTORY_URL
}

func kitDeleteBuildHistoryUrl(id string) string {
	return fmt.Sprintf(KIT_BUILD_HISTORY_ID_URL, id)
}

func extractionsUrl() string {
	return EXTRACTORS_URL
}

func extractionsUploadUrl() string {
	return EXTRACTORS_UPLOAD_URL
}

func extractionsTestUrl() string {
	return EXTRACTORS_TEST_URL
}

func extractionIdUrl(id string) string {
	return fmt.Sprintf(EXTRACTORS_ID_URL, id)
}

func extractionsSyncUrl() string {
	return EXTRACTORS_SYNC_URL
}

func extractionEnginesUrl() string {
	return EXTRACTORS_ENGINES_URL
}

func exploreGenerateUrl() string {
	return EXPLORE_GENERATE_URL
}

func templatesUrl() string {
	return TEMPLATES_URL
}

func templatesGuidUrl(guid uuid.UUID) string {
	return fmt.Sprintf(TEMPLATES_ID_URL, guid)
}

func pivotsUrl() string {
	return PIVOTS_URL
}

func pivotsGuidUrl(guid uuid.UUID) string {
	return fmt.Sprintf(PIVOTS_ID_URL, guid)
}

func userFilesUrl() string {
	return USER_FILES_URL
}

func userFilesIdUrl(id uuid.UUID) string {
	return fmt.Sprintf(USER_FILES_ID_URL, id)
}

func searchLibUrl() string {
	return LIBRARY_URL
}

func searchLibIdUrl(id uuid.UUID) string {
	return fmt.Sprintf(LIBRARY_ID_URL, id.String())
}

func backupUrl() string {
	return BACKUP_URL
}

func deploymentUrl() string {
	return DEPLOYMENT_URL
}

func tokensUrl() string {
	return TOKENS_URL
}

func tokenIdUrl(id uuid.UUID) string {
	return fmt.Sprintf(TOKENS_ID_URL, id.String())
}

func tokenCapabilitiesUrl() string {
	return TOKENS_CAPABILITIES_URL
}
