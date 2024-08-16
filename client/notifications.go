/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import (
	"net/http"
	"time"

	"github.com/gravwell/gravwell/v4/client/types"
)

// MyNotificationCount returns the number of notifications for the current user.
func (c *Client) MyNotificationCount() (int, error) {
	n, err := c.getNotifications(time.Time{}, false)
	if err != nil {
		return -1, err
	}
	return len(n), nil
}

// MyNewNotificationCount returns the number of new notifications since the last read notification.
func (c *Client) MyNewNotificationCount() (int, error) {
	n, err := c.getNotifications(c.sessionData.LastNotificationTime, false)
	if err != nil {
		return -1, err
	}
	return len(n), nil
}

// MyNotifications returns all notifications for the current user.
// Calling MyNotifications updates the last-read notification.
func (c *Client) MyNotifications() (types.NotificationSet, error) {
	return c.getNotifications(time.Time{}, true)
}

// MyNewNotifications returns notifications which have not been previously read.
// Calling MyNewNotifications updates the last-read notification.
func (c *Client) MyNewNotifications() (types.NotificationSet, error) {
	return c.getNotifications(c.sessionData.LastNotificationTime, true)
}

func (c *Client) getNotifications(after time.Time, update bool) (n types.NotificationSet, err error) {
	c.qm.set("after", after.Format("2006-01-02T15:04:05.999999999Z07"))
	if err = c.getStaticURL(notificationsUrl(0), &n); err == nil && update {
		for _, v := range n {
			if v.Sent.After(c.sessionData.LastNotificationTime) {
				c.sessionData.LastNotificationTime = v.Sent
			}
		}
	}
	c.qm.remove("after")
	return
}

// AddSelfTargetedNotification creates a new notification with the given
// type, message, link, and expiration. If expiration time is invalid, the webserver
// will instead set a default expiration.
func (c *Client) AddSelfTargetedNotification(notifType uint32, msg, link string, expiration time.Time) error {
	n := types.Notification{Type: notifType, Msg: msg, Link: link, Expires: expiration}
	return c.methodStaticPushURL(http.MethodPost, notificationsSelfTargetedUrl(), n, nil, nil, nil)
}

// DeleteNotification will delete a notification using a notification ID
func (c *Client) DeleteNotification(id uint64) error {
	return c.deleteStaticURL(notificationsUrl(id), nil)
}

// UpdateNotification will update a notification using a notification ID
func (c *Client) UpdateNotification(id uint64, n types.Notification) error {
	return c.putStaticURL(notificationsUrl(id), n)
}
