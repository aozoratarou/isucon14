package main

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/patrickmn/go-cache"
)

type rideInfo struct {
	RideID                string     `json:"ride_id"`
	PickupCoordinate      Coordinate `json:"pickup_coordinate"`
	DestinationCoordinate Coordinate `json:"destination_coordinate"`
	User                  simpleUser `json:"user"`
}

// このAPIをインスタンス内から一定間隔で叩かせることで、椅子とライドをマッチングさせる
func internalGetMatching(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// MEMO: 一旦最も待たせているリクエストに適当な空いている椅子マッチさせる実装とする。おそらくもっといい方法があるはず…
	ride := &Ride{}
	if err := db.GetContext(ctx, ride, `SELECT * FROM rides WHERE chair_id IS NULL ORDER BY created_at LIMIT 1`); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	// 乗車中のユーザー情報を取得
	user := &User{}
	if err := db.GetContext(ctx, user, "SELECT * FROM users WHERE id = ? FOR SHARE", ride.UserID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	matched := &Chair{}
	empty := false
	for i := 0; i < 10; i++ {
		if err := db.GetContext(ctx, matched, "SELECT * FROM chairs INNER JOIN (SELECT id FROM chairs WHERE is_active = TRUE ORDER BY RAND() LIMIT 1) AS tmp ON chairs.id = tmp.id LIMIT 1"); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			writeError(w, http.StatusInternalServerError, err)
		}

		if err := db.GetContext(ctx, &empty, "SELECT COUNT(*) = 0 FROM (SELECT COUNT(chair_sent_at) = 6 AS completed FROM ride_statuses WHERE ride_id IN (SELECT id FROM rides WHERE chair_id = ?) GROUP BY ride_id) is_completed WHERE completed = FALSE", matched.ID); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if empty {
			break
		}
	}
	if !empty {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if _, err := db.ExecContext(ctx, "UPDATE rides SET chair_id = ? WHERE id = ?", matched.ID, ride.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	i := rideInfo{
		RideID: ride.ID,
		PickupCoordinate: Coordinate{
			Latitude:  ride.PickupLatitude,
			Longitude: ride.PickupLongitude,
		},
		DestinationCoordinate: Coordinate{
			Latitude:  ride.DestinationLatitude,
			Longitude: ride.DestinationLongitude,
		},
		User: simpleUser{
			ID:   user.ID,
			Name: fmt.Sprintf("%s %s", user.Firstname, user.Lastname),
		},
	}
	c.Add("ride:"+ride.ID, &i, cache.DefaultExpiration)

	w.WriteHeader(http.StatusNoContent)
}
