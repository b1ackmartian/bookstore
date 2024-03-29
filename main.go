package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/gorilla/mux"
	vault "github.com/hashicorp/vault/api"
	auth "github.com/hashicorp/vault/api/auth/kubernetes"
	_ "github.com/lib/pq"
	"github.com/spf13/viper"
)

const (
	PORT = "PORT"

	VAULT_ADDR          = "VAULT_ADDR"
	VAULT_ROLE          = "VAULT_ROLE"
	VAULT_KV_MOUNT      = "VAULT_KV_MOUNT"
	VAULT_BOOKSTORE_ENV = "VAULT_BOOKSTORE_ENV"

	KUBE_SVC_ACCT_TOKEN = "KUBE_SVC_ACCT_TOKEN"

	DB_HOST = "DB_HOST"
	DB_PORT = "DB_PORT"
	DB_NAME = "DB_NAME"
	DB_USER = "DB_USER"
	DB_PASS = "DB_PASS"
	DB_SSL  = "DB_SSL"
)

var (
	conf *viper.Viper
)

func init() {
	conf = viper.New()
	conf.AutomaticEnv()

	kvMount := conf.GetString(VAULT_KV_MOUNT)
	bookstoreEnv := conf.GetString(VAULT_BOOKSTORE_ENV)

	client, err := vault.NewClient(vault.DefaultConfig())
	if err != nil {
		log.Fatalf("unable to initialize Vault client: %v", err)
	}

	err = loginVaultKubernetes(client)
	if err != nil {
		log.Println("vault login failed: %w", err)
	}

	secret, err := client.KVv2(kvMount).Get(context.Background(), bookstoreEnv)
	if err != nil {
		log.Fatalf("unable to read secret: %v", err)
	}

	err = conf.MergeConfigMap(secret.Data)
	if err != nil {
		log.Fatalf("unable to merge secret: %v", err)
	}
}

func main() {
	port := conf.GetString(PORT)

	dbUser := conf.GetString(DB_USER)
	dbPass := conf.GetString(DB_PASS)
	dbHost := conf.GetString(DB_HOST)
	dbPort := conf.GetString(DB_PORT)
	dbName := conf.GetString(DB_NAME)
	dbSSL := conf.GetString(DB_SSL)

	dataSourceName := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=%s", dbUser, dbPass, dbHost, dbPort, dbName, dbSSL)

	db, err := sql.Open("postgres", dataSourceName)
	if err != nil {
		log.Fatal(err)
	}

	env := &Env{
		books: BookModel{DB: db},
		app:   App{DB: db},
	}

	router := mux.NewRouter().StrictSlash(true)

	router.HandleFunc("/healthz", env.appHealth).Methods("GET")
	router.HandleFunc("/readyz", env.appReady).Methods("GET")

	router.HandleFunc("/books", env.booksIndex).Methods("GET")
	router.HandleFunc("/books", env.createBook).Methods("POST")
	router.HandleFunc("/books/{isbn}", env.bookByISBN).Methods("GET")

	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%s", port), router))
}

type Env struct {
	app interface {
		CheckDBConn() error
	}
	books interface {
		All() ([]Book, error)
		Get(isbn string) (*Book, error)
		Create(book *Book) error
	}
}

func (env *Env) appHealth(w http.ResponseWriter, r *http.Request) {
	err := env.app.CheckDBConn()
	if err != nil {
		log.Print(err)
		http.Error(w, http.StatusText(500), 500)
	}

	Respond(w, http.StatusText(200), 200)
}

func (env *Env) appReady(w http.ResponseWriter, r *http.Request) {
	err := env.app.CheckDBConn()
	if err != nil {
		log.Print(err)
		http.Error(w, http.StatusText(500), 500)
	}

	Respond(w, http.StatusText(200), 200)
}

func (env *Env) booksIndex(w http.ResponseWriter, r *http.Request) {
	bks, err := env.books.All()
	if err != nil {
		log.Print(err)
		http.Error(w, http.StatusText(500), 500)
		return
	}

	json.NewEncoder(w).Encode(bks)
}

func (env *Env) bookByISBN(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	isbn := vars["isbn"]

	bk, err := env.books.Get(isbn)
	if err != nil {
		log.Print(err)
		http.Error(w, http.StatusText(500), 500)
		return
	}

	json.NewEncoder(w).Encode(bk)
}

func (env *Env) createBook(w http.ResponseWriter, r *http.Request) {
	var bk Book

	err := json.NewDecoder(r.Body).Decode(&bk)
	if err != nil {
		log.Print(err)
		http.Error(w, http.StatusText(400), 400)
		return
	}

	err = env.books.Create(&bk)
	if err != nil {
		log.Print(err)
		http.Error(w, http.StatusText(500), 500)
		return
	}

	json.NewEncoder(w).Encode(&bk)
}

type Book struct {
	Isbn   string  `json:"ISBN"`
	Title  string  `json:"Title"`
	Author string  `json:"Author"`
	Price  float32 `json:"Price"`
}

// Create a custom BookModel type which wraps the sql.DB connection pool.
type BookModel struct {
	DB *sql.DB
}

// Use a method on the custom BookModel type to run the SQL query.
func (m BookModel) All() ([]Book, error) {
	stmt, err := m.DB.Prepare("SELECT * FROM books")
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	rows, err := stmt.Query()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bks []Book

	for rows.Next() {
		var bk Book

		err := rows.Scan(&bk.Isbn, &bk.Title, &bk.Author, &bk.Price)
		if err != nil {
			return nil, err
		}

		bks = append(bks, bk)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	return bks, nil
}

// Use a method on the custom BookModel type to run the SQL query.
func (m BookModel) Get(isbn string) (*Book, error) {
	var bk Book
	stmt, err := m.DB.Prepare("SELECT * FROM books WHERE isbn=$1;")
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	err = stmt.QueryRow(isbn).Scan(&bk.Isbn, &bk.Title, &bk.Author, &bk.Price)
	if err != nil {
		return nil, err
	}

	return &bk, nil
}

func (m BookModel) Create(bk *Book) error {
	stmt, err := m.DB.Prepare("INSERT INTO books (isbn, title, author, price) VALUES ($1, $2, $3, $4);")
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(bk.Isbn, bk.Title, bk.Author, bk.Price)
	if err != nil {
		return err
	}

	return nil
}

func loginVaultKubernetes(client *vault.Client) error {
	vaultRole := conf.GetString(VAULT_ROLE)
	kubeToken := conf.GetString(KUBE_SVC_ACCT_TOKEN)

	k8sAuth, err := auth.NewKubernetesAuth(
		vaultRole,
		auth.WithServiceAccountTokenPath(kubeToken),
	)
	if err != nil {
		return fmt.Errorf("unable to initialize Kubernetes auth method: %w", err)
	}

	authInfo, err := client.Auth().Login(context.Background(), k8sAuth)
	if err != nil {
		return fmt.Errorf("unable to log in with Kubernetes auth: %w", err)
	}
	if authInfo == nil {
		return fmt.Errorf("no auth info was returned after login")
	}

	return nil
}

func Respond(w http.ResponseWriter, text string, code int) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	fmt.Fprintln(w, text)
}

type App struct {
	DB *sql.DB
}

// Use a method on the custom BookModel type to run the SQL query.
func (a App) CheckDBConn() error {
	rows, err := a.DB.Query("SELECT 1")
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var health any

		err := rows.Scan(&health)
		if err != nil {
			return err
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}

	return nil
}
