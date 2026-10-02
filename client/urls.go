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
)

const (
	// login field names
	USER_FIELD string = "User"
	PASS_FIELD string = "Pass"

	// API paths
	LOGIN_URL                        = `/api/login`
	LOGOUT_URL                       = `/api/logout`
	MFA_URL                          = `/api/mfa`
	MFA_CLEAR_ALL_URL                = `/api/mfa/clear`
	MFA_LOGIN_URL                    = `/api/login/mfa`
	MFA_TOTP_SETUP_URL               = `/api/mfa/totp/setup`
	MFA_TOTP_CLEAR_URL               = `/api/mfa/totp/clear`
	MFA_RECOVERY_GENERATE_PATH       = "/api/mfa/recovery-codes/generate"
	TEMP_TOKEN_URL                   = `/api/login/temporary-token`
	REFRESH_TOKEN_URL                = `/api/login/refresh-token`
	USER_INFO_URL                    = `/api/self`
	DESC_URL                         = `/api/stats/system-description`
	STATE_URL                        = `/api/stats/ping`
	STATS_URL                        = `/api/stats/system`
	IDX_URL                          = `/api/stats/indexers`
	INGESTER_URL                     = `/api/stats/ingesters`
	INGESTER_TAIL_URL                = `/api/stats/ingester-tail`
	WELLS_URL                        = `/api/stats/wellStats`
	QUEUE_URL                        = `/api/stats/search-queue`
	STORAGE_URL                      = `/api/stats/indexer-storage`
	STORAGE_INDEXER_URL              = `/api/indexers/%s/wells`
	CALENDAR_URL                     = `/api/stats/storage-calendar`
	CALENDAR_INDEXER_URL             = `/api/indexers/%s/storage-calendar`
	ADD_USER_URL                     = `/api/users`
	USERS_URL                        = `/api/users`
	USERS_LIST_URL                   = `/api/list/users`
	USERS_INFO_URL                   = `/api/users/%d`
	USERS_LOCK_URL                   = `/api/users/%d/lock`
	USERS_LOCKED_URL                 = `/api/users/%d/locked`
	USERS_PREFS_URL                  = `/api/users/%d/preferences`
	USERS_ADMIN_URL                  = `/api/users/%d/admin`
	USERS_ADMIN_SU_PATH              = `/api/users/%d/su`
	USER_SESSIONS_URL                = `/api/users/%d/sessions`
	CHANGE_PASS_URL                  = `/api/users/%d/password`
	USERS_GROUP_URL                  = `/api/users/%d/groups`
	USERS_GROUP_ID_URL               = `/api/users/%d/groups/%d`
	USERS_MFA_CLEAR_URL              = `/api/users/%d/mfa/clear`
	WS_STAT_URL                      = `/api/ws/stats`
	WS_SEARCH_URL                    = `/api/ws/search`
	WS_ATTACH_URL                    = `/api/ws/attach/%s`
	PARSE_URL                        = `/api/validate/query`
	API_VERSION_URL                  = `/api/version`
	GROUP_ID_URL                     = `/api/groups/%d`
	GROUP_MEMBERS_URL                = `/api/groups/%d/members`
	GROUP_URL                        = `/api/groups`
	GROUP_LIST_URL                   = `/api/list/groups`
	SEARCH_PARSE_URL                 = `/api/validate/query`
	SEARCH_CTRL_LIST_URL             = `/api/list/searches`
	SEARCH_CTRL_URL                  = `/api/searches/%s`
	SEARCH_CTRL_ACCESS_URL           = `/api/searches/%s/access`
	SEARCH_CTRL_ATTACH_URL           = `/api/searches/%s/attach`
	SEARCH_CTRL_AX_SUGGESTIONS       = `/api/searches/%s/extractor-suggestions`
	SEARCH_CTRL_BACKGROUND_URL       = `/api/searches/%s/background`
	SEARCH_CTRL_SAVE_URL             = `/api/searches/%s/save`
	SEARCH_CTRL_STOP_URL             = `/api/searches/%s/stop`
	SEARCH_CTRL_DOWNLOAD_URL         = `/api/searches/%s/downloads`
	SEARCH_CTRL_PING_URL             = `/api/searches/%s/ping`
	SEARCH_CTRL_DETACH_URL           = `/api/searches/%s/detach`
	SEARCH_CTRL_MODULES              = `/api/searches/%s/modules`
	SEARCH_CTRL_STATS_URL            = `/api/searches/%s/stats`
	SEARCH_CTRL_STATS_METADATA_URL   = `/api/searches/%s/stats/metadata`
	SEARCH_CTRL_EXPLORE_URL          = `/api/searches/%s/explore/%s`
	SEARCH_CTRL_ENTRIES_URL          = `/api/searches/%s/renderer/%s`
	SEARCH_CTRL_IMPORT_URL           = `/api/import/persistent-search`
	SEARCH_CTRL_LAUNCH_URL           = `/api/searches`
	SEARCH_HISTORY_URL               = `/api/search-history`
	SEARCH_HISTORY_LIST_URL          = `/api/list/search-history`
	SEARCH_HISTORY_ID_URL            = `/api/search-history/%s`
	NOTIFICATIONS_URL                = `/api/list/notifications`
	NOTIFICATIONS_ID_URL             = `/api/notifications/%d`
	NOTIFICATIONS_SELF_TARGETED_URL  = `/api/notifications`
	LOGGING_PATH_URL                 = `/api/logging`
	TEST_URL                         = `/api/test`
	TEST_AUTH_URL                    = `/api/test/authentication`
	DASHBOARD_ID_URL                 = `/api/dashboards/%v`
	DASHBOARDS_URL                   = `/api/dashboards`
	DASHBOARDS_LIST_URL              = `/api/list/dashboards`
	MACROS_URL                       = `/api/macros`
	MACROS_LIST_URL                  = `/api/list/macros`
	MACROS_ID_URL                    = `/api/macros/%s`
	LICENSE_INFO_URL                 = `/api/license`
	LICENSE_SKU_URL                  = `/api/license/sku`
	LICENSE_SERIAL_URL               = `/api/license/serial`
	LICENSE_UPDATE_URL               = `/api/license/update`
	RESOURCES_URL                    = "/api/resources"
	RESOURCES_LIST_URL               = "/api/list/resources"
	RESOURCES_ID_URL                 = "/api/resources/%s"
	RESOURCES_ID_RAW_URL             = "/api/resources/%s/content"
	RESOURCES_ID_CLONE_URL           = "/api/resources/%s/clone"
	RESOURCES_LOOKUP_URL             = "/api/lookup/resources/%s" // may be able to be removed using the new list/resource
	SCHEDULED_SEARCH_URL             = "/api/scheduled-searches"
	SCHEDULED_SEARCH_LIST_URL        = "/api/list/scheduled-searches"
	SCHEDULED_SEARCH_ID_URL          = "/api/scheduled-searches/%s"
	SCHEDULED_SEARCH_RESULTS_ID_URL  = "/api/scheduled-searches/%s/results"
	SCHEDULED_SEARCH_DEBUG_ID_URL    = "/api/scheduled-searches/%s/debug"
	SCHEDULED_SEARCH_CANCEL_ID_URL   = "/api/scheduled-searches/%s/cancel"
	SCHEDULED_SEARCH_CHECKIN_URL     = "/api/search-agent/checkin"
	SCHEDULED_SCRIPT_URL             = "/api/scheduled-scripts"
	SCHEDULED_SCRIPT_LIST_URL        = "/api/list/scheduled-scripts"
	SCHEDULED_SCRIPT_ID_URL          = "/api/scheduled-scripts/%s"
	SCHEDULED_SCRIPT_RESULTS_ID_URL  = "/api/scheduled-scripts/%s/results"
	SCHEDULED_SCRIPT_DEBUG_ID_URL    = "/api/scheduled-scripts/%s/debug"
	SCHEDULED_SCRIPT_CANCEL_ID_URL   = "/api/scheduled-scripts/%s/cancel"
	SCHEDULED_SCRIPT_CHECKIN_URL     = "/api/scheduled-scripts/checkin"
	SCHEDULED_SCRIPT_PARSE           = "/api/validate/scheduled-script"
	FLOW_URL                         = "/api/flows"
	FLOW_LIST_URL                    = "/api/list/flows"
	FLOW_ID_URL                      = "/api/flows/%v"
	FLOW_RESULTS_ID_URL              = "/api/flows/%s/results"
	FLOW_DEBUG_ID_URL                = "/api/flows/%s/debug"
	FLOW_CANCEL_ID_URL               = "/api/flows/%s/cancel"
	FLOW_PARSE_URL                   = "/api/validate/flow"
	MAIL_URL                         = "/api/mail"
	MAIL_CONFIGURE_URL               = `/api/self/mail-configuration`
	JSON_INGEST_URL                  = "/api/ingest/json"
	LINES_INGEST_URL                 = "/api/ingest/lines"
	INTERNAL_INGEST_URL              = "/api/ingest/internal"
	TEST_INGEST_URL                  = "/api/ingest/test"
	TAGS_URL                         = "/api/list/tags"
	INDEXER_MANAGE_ADD_URL           = "/api/indexers"
	KIT_URL                          = `/api/kits`
	KIT_LIST_URL                     = `/api/list/kits`
	KIT_ID_URL                       = `/api/kits/%s`
	KIT_BUILD_URL                    = `/api/kit-builds`
	KIT_BUILD_ID_URL                 = `/api/kit-builds/%s`
	KIT_STATUS_URL                   = `/api/list/kit-installs`
	KIT_STATUS_ID_URL                = `/api/kit-installs/%v`
	KIT_REMOTE_LIST_URL              = `/api/list/remote-kits`
	KIT_BUILD_HISTORY_URL            = `/api/kit-build-requests`
	KIT_BUILD_HISTORY_LIST_URL       = `/api/list/kit-build-requests`
	KIT_BUILD_HISTORY_ID_URL         = `/api/kit-build-requests/%s`
	EXTRACTORS_URL                   = `/api/auto-extractors`
	EXTRACTORS_LIST_URL              = `/api/list/auto-extractors`
	EXTRACTORS_UPLOAD_URL            = `/api/auto-extractors/upload`
	EXTRACTORS_TEST_URL              = `/api/validate/auto-extractor`
	EXTRACTORS_ID_URL                = `/api/auto-extractors/%s`
	EXTRACTORS_FIND_URL              = `/api/tags/%s/auto-extractor`
	EXTRACTORS_ENGINES_URL           = `/api/info/extractor-engines`
	TEMPLATES_URL                    = "/api/templates"
	TEMPLATES_LIST_URL               = "/api/list/templates"
	TEMPLATES_ID_URL                 = "/api/templates/%s"
	TEMPLATES_ID_DETAILS_URL         = "/api/templates/%s/details"
	ACTIONABLES_URL                  = "/api/actionables"
	ACTIONABLES_LIST_URL             = "/api/list/actionables"
	ACTIONABLES_ID_URL               = "/api/actionables/%s"
	FILES_URL                        = "/api/files"
	FILES_LIST_URL                   = "/api/list/files"
	FILES_ID_URL                     = "/api/files/%s"
	FILES_ID_RAW_URL                 = "/api/files/%s/content"
	LIBRARY_URL                      = "/api/saved-queries"
	LIBRARY_ID_URL                   = "/api/saved-queries/%s"
	LIBRARY_LIST_URL                 = "/api/list/saved-queries"
	LIBS_URL                         = `/api/libs`
	CAPABILITY_LIST_URL              = `/api/info/capabilities`
	CAPABILITY_TEMPLATE_LIST_URL     = `/api/info/capability-templates`
	CAPABILITY_CURRENT_USER_LIST_URL = `/api/self/capabilities`
	CAPABILITY_CURRENT_USER_WHY_URL  = `/api/self/capability-explanations`
	CAPABILITY_USER_URL              = `/api/users/%d/capabilities`
	CAPABILITY_USER_WHY_URL          = `/api/users/%d/capability-explanations`
	CAPABILITY_GROUP_URL             = `/api/groups/%d/capabilities`
	GROUP_TAG_ACCESS_URL             = `/api/groups/%d/tags`
	USER_TAG_ACCESS_URL              = `/api/users/%d/tags`
	PLAYBOOKS_URL                    = `/api/playbooks`
	PLAYBOOKS_LIST_URL               = `/api/list/playbooks`
	PLAYBOOKS_ID_URL                 = `/api/playbooks/%s`
	BACKUP_URL                       = `/api/backup`
	DEPLOYMENT_URL                   = `/api/info/deployment`
	TOKENS_URL                       = `/api/tokens`
	TOKENS_LIST_URL                  = `/api/list/tokens`
	TOKENS_ID_URL                    = `/api/tokens/%s`
	TOKENS_ID_REGEN_URL              = `/api/tokens/%s/regenerate`
	TOKENS_CAPABILITIES_URL          = `/api/info/token-capabilities`
	SECRETS_URL                      = `/api/secrets`
	SECRETS_LIST_URL                 = `/api/list/secrets`
	SECRETS_ID_URL                   = `/api/secrets/%s`
	SECRETS_ID_VALUE_URL             = `/api/secrets/%s/value`
	SECRETS_ID_FULL_URL              = `/api/secrets/%s/full`
	SETTINGS_URL                     = `/api/info/settings`
	INGESTERS_BULK_TRACKING_URL      = `/api/ingesters/tracking`
	ALERTS_URL                       = `/api/alerts`
	ALERTS_LIST_URL                  = `/api/list/alerts`
	ALERTS_ID_URL                    = `/api/alerts/%s`
	ALERTS_ID_SAMPLE_URL             = `/api/alerts/%s/sample`
	ALERTS_VALIDATE_DISPATCHER_URL   = `/api/validate/alert-dispatcher`
	ALERTS_VALIDATE_CONSUMER_URL     = `/api/validate/alert-consumer`
	USER_PREFERENCES_URL             = `/api/user-preferences`
	USER_PREFERENCES_LIST_URL        = `/api/list/user-preferences`
	USER_PREFERENCES_ID_URL          = `/api/user-preferences/%s`
	LIST_URL                         = `/api/list/assets`
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

func usersGroupIdUrl(uid, gid int32) string {
	return fmt.Sprintf(USERS_GROUP_ID_URL, uid, gid)
}

func searchHistoryIdUrl(id string) string {
	return fmt.Sprintf(SEARCH_HISTORY_ID_URL, id)
}

func searchHistoryListUrl() string {
	return SEARCH_HISTORY_LIST_URL
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

func dashboardIdUrl(id string) string {
	return fmt.Sprintf(DASHBOARD_ID_URL, id)
}

func usersUrl() string {
	return USERS_URL
}

func searchCtrlAccessUrl(id string) string {
	return fmt.Sprintf(SEARCH_CTRL_ACCESS_URL, id)
}

func searchCtrlBackgroundUrl(id string) string {
	return fmt.Sprintf(SEARCH_CTRL_BACKGROUND_URL, id)
}

func searchCtrlSaveUrl(id string) string {
	return fmt.Sprintf(SEARCH_CTRL_SAVE_URL, id)
}

func searchCtrlDownloadUrl(id string) string {
	return fmt.Sprintf(SEARCH_CTRL_DOWNLOAD_URL, id)
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

func sessionsUrl(id int32) string {
	return fmt.Sprintf(USER_SESSIONS_URL, id)
}

func preferencesUrl(id int32) string {
	return fmt.Sprintf(USERS_PREFS_URL, id)
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
	return RESOURCES_URL
}

func resourcesIdUrl(id string) string {
	return fmt.Sprintf(RESOURCES_ID_URL, id)
}

func resourcesIdRawUrl(id string) string {
	return fmt.Sprintf(RESOURCES_ID_RAW_URL, id)
}

func resourcesLookupUrl(name string) string {
	return fmt.Sprintf(RESOURCES_LOOKUP_URL, name)
}

func resourcesCloneUrl(id string) string {
	return fmt.Sprintf(RESOURCES_ID_CLONE_URL, id)
}

func scheduledSearchUrl() string {
	return SCHEDULED_SEARCH_URL
}

func scheduledSearchIdUrl(id string) string {
	return fmt.Sprintf(SCHEDULED_SEARCH_ID_URL, id)
}

func scheduledSearchResultsIdUrl(id string) string {
	return fmt.Sprintf(SCHEDULED_SEARCH_RESULTS_ID_URL, id)
}

func scheduledSearchDebugIdUrl(id string) string {
	return fmt.Sprintf(SCHEDULED_SEARCH_DEBUG_ID_URL, id)
}

func scheduledSearchCancelIdUrl(id string) string {
	return fmt.Sprintf(SCHEDULED_SEARCH_CANCEL_ID_URL, id)
}

func scheduledScriptUrl() string {
	return SCHEDULED_SCRIPT_URL
}

func scheduledScriptParseUrl() string {
	return SCHEDULED_SCRIPT_PARSE
}

func scheduledScriptIdUrl(id string) string {
	return fmt.Sprintf(SCHEDULED_SCRIPT_ID_URL, id)
}

func scheduledScriptResultsIdUrl(id string) string {
	return fmt.Sprintf(SCHEDULED_SCRIPT_RESULTS_ID_URL, id)
}

func scheduledScriptDebugIdUrl(id string) string {
	return fmt.Sprintf(SCHEDULED_SCRIPT_DEBUG_ID_URL, id)
}

func scheduledScriptCancelIdUrl(id string) string {
	return fmt.Sprintf(SCHEDULED_SCRIPT_CANCEL_ID_URL, id)
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

func flowIdUrl(id any) string {
	return fmt.Sprintf(FLOW_ID_URL, id)
}

func flowResultsIdUrl(id string) string {
	return fmt.Sprintf(FLOW_RESULTS_ID_URL, id)
}

func flowDebugIdUrl(id string) string {
	return fmt.Sprintf(FLOW_DEBUG_ID_URL, id)
}

func flowCancelIdUrl(id string) string {
	return fmt.Sprintf(FLOW_CANCEL_ID_URL, id)
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
	return WELLS_URL
}

func searchQueueUrl() string {
	return QUEUE_URL
}

func macroIDUrl(id string) string {
	return fmt.Sprintf(MACROS_ID_URL, id)
}

func playbookUrl(id string) string {
	return fmt.Sprintf(PLAYBOOKS_ID_URL, id)
}

func kitUrl() string {
	return KIT_URL
}

func remoteKitUrl(all bool) string {
	if all {
		return KIT_REMOTE_LIST_URL + "?all=true"
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

func kitStatusIdUrl(id int) string {
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

func extractionFindUrl(tag string) string {
	return fmt.Sprintf(EXTRACTORS_FIND_URL, tag)
}

func extractionEnginesUrl() string {
	return EXTRACTORS_ENGINES_URL
}

func exploreGenerateUrl(searchId string) string {
	return fmt.Sprintf(SEARCH_CTRL_AX_SUGGESTIONS, searchId)
}

func templatesUrl() string {
	return TEMPLATES_URL
}

func templateUrl(id string) string {
	return fmt.Sprintf(TEMPLATES_ID_URL, id)
}

func actionableIdUrl(id string) string {
	return fmt.Sprintf(ACTIONABLES_ID_URL, id)
}

func filesUrl() string {
	return FILES_URL
}

func filesIdUrl(id string) string {
	return fmt.Sprintf(FILES_ID_URL, id)
}

func filesIdRawUrl(id string) string {
	return fmt.Sprintf(FILES_ID_RAW_URL, id)
}

func searchLibUrl() string {
	return LIBRARY_URL
}

func searchLibIdUrl(id string) string {
	return fmt.Sprintf(LIBRARY_ID_URL, id)
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

func tokenIdUrl(id string) string {
	return fmt.Sprintf(TOKENS_ID_URL, id)
}

func tokenIDRegenURL(id string) string {
	return fmt.Sprintf(TOKENS_ID_REGEN_URL, id)
}

func tokenCapabilitiesUrl() string {
	return TOKENS_CAPABILITIES_URL
}

func secretsUrl() string {
	return SECRETS_URL
}

func secretIdUrl(id string) string {
	return fmt.Sprintf(SECRETS_ID_URL, id)
}

func secretIdValueUrl(id string) string {
	return fmt.Sprintf(SECRETS_ID_VALUE_URL, id)
}

func secretIdFullUrl(id string) string {
	return fmt.Sprintf(SECRETS_ID_FULL_URL, id)
}

func searchLaunchUrl() string {
	return SEARCH_CTRL_LAUNCH_URL
}

func searchPingUrl(id string) string {
	return fmt.Sprintf(SEARCH_CTRL_PING_URL, id)
}

func searchDetachUrl(id string) string {
	return fmt.Sprintf(SEARCH_CTRL_DETACH_URL, id)
}

func searchStatsMetadataUrl(id string) string {
	return fmt.Sprintf(SEARCH_CTRL_STATS_METADATA_URL, id)
}

func searchStatsUrl(id string) string {
	return fmt.Sprintf(SEARCH_CTRL_STATS_URL, id)
}

func searchStatsModules(id string) string {
	return fmt.Sprintf(SEARCH_CTRL_MODULES, id)
}

func searchExploreUrl(id, rndr string) string {
	return fmt.Sprintf(SEARCH_CTRL_EXPLORE_URL, id, rndr)
}

func searchEntriesUrl(id, rndr string) string {
	return fmt.Sprintf(SEARCH_CTRL_ENTRIES_URL, id, rndr)
}

func searchParseUrl() string {
	return SEARCH_PARSE_URL
}

func searchAttachUrl(id string) string {
	return fmt.Sprintf(SEARCH_CTRL_ATTACH_URL, id)
}

func alertsUrl() string {
	return ALERTS_URL
}

func alertsIdUrl(id string) string {
	return fmt.Sprintf(ALERTS_ID_URL, id)
}

func alertsIdSampleEventUrl(id string) string {
	return fmt.Sprintf(ALERTS_ID_SAMPLE_URL, id)
}

func alertsValidateDispatcherUrl() string {
	return ALERTS_VALIDATE_DISPATCHER_URL
}

func alertsValidateConsumerUrl() string {
	return ALERTS_VALIDATE_CONSUMER_URL
}

func totpSetupUrl() string {
	return MFA_TOTP_SETUP_URL
}

func totpClearUrl() string {
	return MFA_TOTP_CLEAR_URL
}

func mfaLoginUrl() string {
	return MFA_LOGIN_URL
}

func mfaUrl() string {
	return MFA_URL
}

func clearUserMFAUrl(uid int32) string {
	return fmt.Sprintf(USERS_MFA_CLEAR_URL, uid)
}

func mfaClearAllUrl() string {
	return MFA_CLEAR_ALL_URL
}

func mfaGenerateRecoveryCodesUrl() string {
	return MFA_RECOVERY_GENERATE_PATH
}

func userPreferenceUrl(id string) string {
	return fmt.Sprintf(USER_PREFERENCES_ID_URL, id)
}
