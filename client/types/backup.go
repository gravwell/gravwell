/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

type BackupConfig struct {
	IncludeSS     bool   // Include scheduled searches
	OmitSensitive bool   // Omit sensitive items
	Password      string // password to use when encrypting backup file
}

type BackupResponse struct {
	DownloadID string
}
