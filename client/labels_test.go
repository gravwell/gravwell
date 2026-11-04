/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import (
	"encoding/json/v2"
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/gravwell/gravwell/v4/client/types"
)

func TestListLabels(t *testing.T) {
	t.Run("sends options and decodes the page", func(t *testing.T) {
		var got types.QueryOptions
		mux := http.NewServeMux()
		mux.HandleFunc("POST "+LIST_LABELS_URL, func(w http.ResponseWriter, r *http.Request) {
			if err := json.UnmarshalRead(r.Body, &got); err != nil {
				t.Errorf("bad request body: %v", err)
			}
			w.Write([]byte(`{"Results":[{"Value":"alpha"},{"Value":"beta"}],"CursorNext":"beta","Offset":0,"TotalCount":3,"Type":"label","AvailableFilters":[{"Key":"Value"}]}`))
		})
		c := newTestClient(t, mux)

		opts := types.QueryOptions{
			Limit:   2,
			OrderBy: "Value",
			Filters: []types.Filter{{Key: "Value", Operation: "LIKE", Values: []any{"%a%"}}},
		}
		resp, err := c.ListLabels(opts)
		if err != nil {
			t.Fatalf("ListLabels: %v", err)
		}
		if got.Limit != 2 || got.OrderBy != "Value" || len(got.Filters) != 1 || got.Filters[0].Key != "Value" {
			t.Errorf("server saw options %+v", got)
		}
		want := []types.Label{{Value: "alpha"}, {Value: "beta"}}
		if !reflect.DeepEqual(resp.Results, want) {
			t.Errorf("Results = %+v, want %+v", resp.Results, want)
		}
		if resp.CursorNext != "beta" || resp.TotalCount != 3 || resp.Type != "label" {
			t.Errorf("base list fields = %+v", resp.BaseListResponse)
		}
		if len(resp.AvailableFilters) != 1 || resp.AvailableFilters[0].Key != "Value" {
			t.Errorf("AvailableFilters = %+v", resp.AvailableFilters)
		}
	})

	t.Run("non-200 status surfaces as a ClientError", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("POST "+LIST_LABELS_URL, func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, `{"Err":"unsupported option"}`, http.StatusBadRequest)
		})
		c := newTestClient(t, mux)

		_, err := c.ListLabels(types.QueryOptions{All: true})
		var ce *ClientError
		if !errors.As(err, &ce) || ce.StatusCode != http.StatusBadRequest {
			t.Fatalf("err = %v, want *ClientError with status %d", err, http.StatusBadRequest)
		}
	})
}
