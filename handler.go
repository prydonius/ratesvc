/*
Copyright (c) 2017 Bitnami

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/kubeapps/ratesvc/response"
	log "github.com/sirupsen/logrus"

	jwt "github.com/dgrijalva/jwt-go"
	"gopkg.in/mgo.v2/bson"
)

const itemCollection = "items"

type item struct {
	// Instead of bson.ObjectID, we use a human-friendly identifier (e.g. "stable/wordpress")
	ID string `json:"id" bson:"_id,omitempty"`
	// Type could be "chart", "function", etc.
	Type string `json:"type"`
	// List of IDs of Stargazers that will be stored in the database
	StargazersIDs []bson.ObjectId `json:"-" bson:"stargazers_ids"`
	// Count of the Stargazers which is only exposed in the JSON response
	StargazersCount int `json:"stargazers_count" bson:"-"`
	// Whether the current user has starred the item, only exposed in the JSON response
	HasStarred bool `json:"has_starred" bson:"-"`
}

// GetStars returns a list of starred items
func GetStars(w http.ResponseWriter, req *http.Request) {
	db, closer := dbSession.DB()
	defer closer()
	var items []*item
	if err := db.C(itemCollection).Find(nil).All(&items); err != nil {
		log.WithError(err).Error("could not fetch all items")
		response.NewErrorResponse(http.StatusInternalServerError, "could not fetch all items").Write(w)
		return
	}
	for _, it := range items {
		it.StargazersCount = len(it.StargazersIDs)
		if currentUser, err := getCurrentUserID(req); err == nil {
			for _, id := range it.StargazersIDs {
				if id == currentUser {
					it.HasStarred = true
					break
				}
			}
		}
	}
	response.NewDataResponse(items).Write(w)
}

// UpdateStar updates the HasStarred attribute on an item
func UpdateStar(w http.ResponseWriter, req *http.Request) {
	db, closer := dbSession.DB()
	defer closer()

	uid, err := getCurrentUserID(req)
	if err != nil {
		response.NewErrorResponse(http.StatusUnauthorized, "unauthorized").Write(w)
	}

	// Params validation
	var it *item
	if err := json.NewDecoder(req.Body).Decode(&it); err != nil {
		log.WithError(err).Error("could not parse request body")
		response.NewErrorResponse(http.StatusBadRequest, "could not parse request body").Write(w)
		return
	}

	if it.ID == "" {
		response.NewErrorResponse(http.StatusBadRequest, "id missing in request body").Write(w)
		return
	}

	if it.Type == "" {
		it.Type = "chart"
	}

	if _, err := db.C(itemCollection).UpsertId(it.ID, it); err != nil {
		log.WithError(err).Error("could not update item")
		response.NewErrorResponse(http.StatusInternalServerError, "internal server error").Write(w)
		return
	}

	op := "$pull"
	if it.HasStarred {
		op = "$push"
	}

	if err := db.C(itemCollection).UpdateId(it.ID, bson.M{op: bson.M{"stargazers_ids": uid}}); err != nil {
		log.WithError(err).Error("could not update item")
		response.NewErrorResponse(http.StatusInternalServerError, "internal server error").Write(w)
		return
	}

	response.NewDataResponse(it).WithCode(http.StatusCreated).Write(w)
}

// GetComments returns a list of comments for an item
func GetComments(w http.ResponseWriter, req *http.Request) {
	panic("not implemented")
}

// CreateComment creates a comment for an item
func CreateComment(w http.ResponseWriter, req *http.Request) {
	panic("not implemented")
}

type userClaims struct {
	ID bson.ObjectId
	jwt.StandardClaims
}

var getCurrentUserID = func(req *http.Request) (bson.ObjectId, error) {
	jwtKey, ok := os.LookupEnv("JWT_KEY")
	if !ok {
		return "", errors.New("JWT_KEY not set")
	}

	cookie, err := req.Cookie("ka_auth")
	if err != nil {
		return "", err
	}

	token, err := jwt.ParseWithClaims(cookie.Value, &userClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(jwtKey), nil
	})
	if err != nil {
		return "", err
	}

	if claims, ok := token.Claims.(*userClaims); ok && token.Valid {
		return claims.ID, nil
	}
	return "", errors.New("invalid token")
}
