package main

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
)

const ValidAccessToken = "secret_token"

var DatasetPath = "dataset.xml"

type xmlRow struct {
	ID        int    `xml:"id"`
	Age       int    `xml:"age"`
	FirstName string `xml:"first_name"`
	LastName  string `xml:"last_name"`
	Gender    string `xml:"gender"`
	About     string `xml:"about"`
}

type xmlRoot struct {
	Rows []xmlRow `xml:"row"`
}

var errBadOrderField = errors.New(ErrorBadOrderField)

func SearchServer(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("AccessToken") != ValidAccessToken {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	params, err := parseParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	users, err := loadUsers(DatasetPath)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	users = filterUsers(users, params.Query)
	sortUsers(users, params.OrderField, params.OrderBy)
	users = paginate(users, params.Offset, params.Limit)

	writeJSON(w, http.StatusOK, users)
}

func parseParams(r *http.Request) (SearchRequest, error) {
	q := r.URL.Query()
	p := SearchRequest{Query: q.Get("query")}

	var err error
	if p.Limit, err = strconv.Atoi(q.Get("limit")); err != nil || p.Limit < 0 {
		return p, errors.New("invalid limit")
	}
	if p.Offset, err = strconv.Atoi(q.Get("offset")); err != nil || p.Offset < 0 {
		return p, errors.New("invalid offset")
	}
	if p.OrderBy, err = strconv.Atoi(q.Get("order_by")); err != nil ||
		(p.OrderBy != OrderByAsc && p.OrderBy != OrderByAsIs && p.OrderBy != OrderByDesc) {
		return p, errors.New("invalid order_by")
	}

	switch strings.ToLower(q.Get("order_field")) {
	case "", "name":
		p.OrderField = "name"
	case "id":
		p.OrderField = "id"
	case "age":
		p.OrderField = "age"
	default:
		return p, errBadOrderField
	}
	return p, nil
}

func loadUsers(path string) ([]User, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var root xmlRoot
	if err := xml.NewDecoder(f).Decode(&root); err != nil {
		return nil, err
	}

	users := make([]User, 0, len(root.Rows))
	for _, row := range root.Rows {
		users = append(users, User{
			ID:     row.ID,
			Name:   row.FirstName + " " + row.LastName,
			Age:    row.Age,
			About:  row.About,
			Gender: row.Gender,
		})
	}
	return users, nil
}

func filterUsers(users []User, query string) []User {
	if query == "" {
		return users
	}
	res := make([]User, 0, len(users))
	for _, u := range users {
		if strings.Contains(u.Name, query) || strings.Contains(u.About, query) {
			res = append(res, u)
		}
	}
	return res
}

func sortUsers(users []User, field string, orderBy int) {
	if orderBy == OrderByAsIs {
		return
	}
	less := func(i, j int) bool {
		switch field {
		case "id":
			return users[i].ID < users[j].ID
		case "age":
			return users[i].Age < users[j].Age
		default:
			return users[i].Name < users[j].Name
		}
	}
	if orderBy == OrderByDesc {
		sort.SliceStable(users, func(i, j int) bool { return less(j, i) })
		return
	}
	sort.SliceStable(users, less)
}

// limit == 0 означает "без ограничения"
func paginate(users []User, offset, limit int) []User {
	if offset >= len(users) {
		return []User{}
	}
	users = users[offset:]
	if limit > 0 && limit < len(users) {
		users = users[:limit]
	}
	return users
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, SearchErrorResponse{Error: msg})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := io.WriteString(w, string(data)); err != nil {
		return
	}
}
