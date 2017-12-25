/*
Copyright 2017 Nirmal Kumar

This file is part of LibreRead.

LibreRead is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

LibreRead is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with LibreRead.  If not, see <http://www.gnu.org/licenses/>.
*/

package libreread

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/blevesearch/bleve"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis"
	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/gomail.v2"
)

type Env struct {
	db          *sql.DB
	RedisClient *redis.Client
}

const (
	PORT_DEFAULT           = "8080"
	PORT_ENV               = "LIBREREAD_PORT"
	ENABLE_ES_ENV          = "LIBREREAD_ELASTICSEARCH"
	ENABLE_ES_DEFAULT      = "0"
	ESPATH_ENV             = "LIBREREAD_ES_PATH"
	ESPATH_DEFAULT         = "http://localhost:9200"
	REDISPATH_ENV          = "LIBREREAD_REDIS_PATH"
	REDISPATH_DEFAULT      = "localhost:6379"
	ASSETPATH_ENV          = "LIBREREAD_ASSET_PATH"
	ASSETPATH_DEFAULT      = "."
	DOMAIN_ADDRESS_ENV     = "LIBREREAD_DOMAIN_ADDRESS"
	DOMAIN_ADDRESS_DEFAULT = ""
	SMTP_SERVER_ENV        = "LIBREREAD_SMTP_SERVER"
	SMTP_SERVER_DEFAULT    = ""
	SMTP_PORT_ENV          = "LIBREREAD_SMTP_PORT"
	SMTP_PORT_DEFAULT      = ""
	SMTP_ADDRESS_ENV       = "LIBREREAD_SMTP_ADDRESS"
	SMTP_ADDRESS_DEFAULT   = ""
	SMTP_PASSWORD_ENV      = "LIBREREAD_SMTP_PASSWORD"
	SMTP_PASSWORD_DEFAULT  = ""
)

var (
	EnableES      = ENABLE_ES_DEFAULT
	ESPath        = ESPATH_DEFAULT
	RedisPath     = REDISPATH_DEFAULT
	ServerPort    = PORT_DEFAULT
	AssetPath     = ASSETPATH_DEFAULT
	DomainAddress = DOMAIN_ADDRESS_DEFAULT
	SMTPServer    = SMTP_SERVER_DEFAULT
	SMTPPort      = SMTP_PORT_DEFAULT
	SMTPAddress   = SMTP_ADDRESS_DEFAULT
	SMTPPassword  = SMTP_PASSWORD_DEFAULT
)

func init() {
	fmt.Println("Running init ...")
	EnableES = _GetEnv(ENABLE_ES_ENV, ENABLE_ES_DEFAULT)
	ESPath = _GetEnv(ESPATH_ENV, ESPATH_DEFAULT)
	RedisPath = _GetEnv(REDISPATH_ENV, REDISPATH_DEFAULT)
	ServerPort = _GetEnv(PORT_ENV, PORT_DEFAULT)
	AssetPath = _GetEnv(ASSETPATH_ENV, ASSETPATH_DEFAULT)
	DomainAddress = _GetEnv(DOMAIN_ADDRESS_ENV, DOMAIN_ADDRESS_DEFAULT)
	SMTPServer = _GetEnv(SMTP_SERVER_ENV, SMTP_SERVER_DEFAULT)
	SMTPPort = _GetEnv(SMTP_PORT_ENV, SMTP_PORT_DEFAULT)
	SMTPAddress = _GetEnv(SMTP_ADDRESS_ENV, SMTP_ADDRESS_DEFAULT)
	SMTPPassword = _GetEnv(SMTP_PASSWORD_ENV, SMTP_PASSWORD_DEFAULT)

	fmt.Printf("Enable Elasticsearch: %s\n", EnableES)
	fmt.Printf("ElasticSearch: %s\n", ESPath)
	fmt.Printf("Redis: %s\n", RedisPath)
	fmt.Printf("Asset path: %s\n", AssetPath)
	fmt.Printf("Domain address: %s\n", DomainAddress)
	fmt.Printf("SMTP server: %s\n", SMTPServer)
	fmt.Printf("SMTP port: %s\n", SMTPPort)
	fmt.Printf("SMTP address: %s\n", SMTPAddress)
}

func StartServer() {
	r := gin.Default()

	// Initiate session management (cookie-based)
	store := sessions.NewCookieStore([]byte("secret"))
	r.Use(sessions.Sessions("mysession", store))

	// Serve static files
	r.Static("/static", path.Join(AssetPath, "static"))
	r.Static("/uploads", "./uploads")

	// HTML rendering
	r.LoadHTMLGlob(path.Join(AssetPath, "templates/*"))

	// Open sqlite3 database
	db, err := sql.Open("sqlite3", "./libreread.db")
	CheckError(err)

	// Close sqlite3 database when all the functions are done
	defer db.Close()

	// Create user table
	// Table: user
	// -------------------------------------------------
	// Fields: id, name, email, password_hash, confirmed
	// -------------------------------------------------
	stmt, err := db.Prepare("CREATE TABLE IF NOT EXISTS `user` " +
		"(`id` INTEGER PRIMARY KEY AUTOINCREMENT, `name` VARCHAR(255) NOT NULL," +
		" `email` VARCHAR(255) UNIQUE NOT NULL, `password_hash` VARCHAR(255) NOT NULL," +
		" `confirmed` INTEGER DEFAULT 0, `forgot_password_token` VARCHAR(255))")
	CheckError(err)

	_, err = stmt.Exec()
	CheckError(err)

	// Create confirm table
	// Table: confirm
	// -----------------------------------------------------------------------------------------------------------
	// Fields: id, token, date_generated, date_expires, date_used, used, user_id (foreign key referencing user id)
	// -----------------------------------------------------------------------------------------------------------
	stmt, err = db.Prepare("CREATE TABLE IF NOT EXISTS `confirm` (`id` INTEGER PRIMARY KEY AUTOINCREMENT," +
		" `token` VARCHAR(255) NOT NULL, `date_generated` VARCHAR(255) NOT NULL," +
		" `date_expires` VARCHAR(255) NOT NULL, `date_used` VARCHAR(255)," +
		" `used` INTEGER DEFAULT 0, `user_id` INTEGER NOT NULL)")
	CheckError(err)

	_, err = stmt.Exec()
	CheckError(err)

	// Create book table
	// Table: book
	// --------------------------------------------------------------------------------------------------
	// Fields: id, title, filename, author, url, cover, pages, current_page, format, uploaded_on, user_id
	// --------------------------------------------------------------------------------------------------
	stmt, err = db.Prepare("CREATE TABLE IF NOT EXISTS `book` (`id` INTEGER PRIMARY KEY AUTOINCREMENT," +
		" `title` VARCHAR(255) NOT NULL, `filename` VARCHAR(255) NOT NULL, `file_path` VARCHAR(255) NOT NULL," +
		" `author` VARCHAR(255) NOT NULL, `url` VARCHAR(255) NOT NULL, `cover` VARCHAR(255) NOT NULL," +
		" `pages` INTEGER NOT NULL, `current_page` INTEGER DEFAULT 0, `format` VARCHAR(255) NOT NULL," +
		" `uploaded_on` VARCHAR(255) NOT NULL, `user_id` INTEGER NOT NULL)")
	CheckError(err)

	_, err = stmt.Exec()
	CheckError(err)

	// Create currently_reading table
	// Table: currently_reading
	// ---------------------------------------
	// Fields: id, book_id, user_id, date_read
	// ---------------------------------------
	stmt, err = db.Prepare("CREATE TABLE IF NOT EXISTS `currently_reading` (`id` INTEGER PRIMARY KEY AUTOINCREMENT," +
		" `book_id` INTEGER NOT NULL, `user_id` INTEGER NOT NULL, `date_read` VARCHAR(255) NOT NULL)")
	CheckError(err)

	_, err = stmt.Exec()
	CheckError(err)

	// Create collection table
	// Table: collection
	// ----------------------------------------------
	// Fields: id, title, description, books, user_id
	// ----------------------------------------------
	stmt, err = db.Prepare("CREATE TABLE IF NOT EXISTS `collection` (`id` INTEGER PRIMARY KEY AUTOINCREMENT," +
		" `title` VARCHAR(255) NOT NULL, `description` VARCHAR(1200) NOT NULL, `books` VARCHAR(1200) NOT NULL," +
		" `cover` VARCHAR(255) NULL, `user_id` INTEGER NOT NULL)")
	CheckError(err)

	_, err = stmt.Exec()
	CheckError(err)

	// Create PDF Highlighter table
	// Table: pdf_highlighter
	// ----------------------------------------------------------------
	// Fields: id, book_id, user_id, highlight_color, highlight_comment
	// ----------------------------------------------------------------
	stmt, err = db.Prepare("CREATE TABLE IF NOT EXISTS `pdf_highlighter` (`id` INTEGER PRIMARY KEY AUTOINCREMENT," +
		" `book_id` INTEGER NOT NULL, `user_id` INTEGER NOT NULL, `highlight_color` VARCHAR(255) NOT NULL," +
		" `highlight_top` VARCHAR(255) NOT NULL, `highlight_comment` VARCHAR(255) NOT NULL)")
	CheckError(err)

	_, err = stmt.Exec()
	CheckError(err)

	// Create PDF Highlighter HTML table
	// Table: pdf_highlighter_detail
	// ---------------------------------------------------------------
	// Fields: id, highlighter_id, page_index, div_index, html_content
	// ---------------------------------------------------------------
	stmt, err = db.Prepare("CREATE TABLE IF NOT EXISTS `pdf_highlighter_detail` (`id` INTEGER PRIMARY KEY AUTOINCREMENT," +
		" `highlighter_id` INTEGER NOT NULL, `page_index` VARCHAR(255) NOT NULL, `div_index` VARCHAR(255) NOT NULL," +
		" `html_content` VARCHAR(1200) NOT NULL)")
	CheckError(err)

	_, err = stmt.Exec()
	CheckError(err)

	// Bleve settings
	// Check if bleve setting already exists. If not create a new setting.
	if _, err := os.Stat("./lr_index.bleve"); os.IsNotExist(err) {
		mapping := bleve.NewIndexMapping()
		index, err := bleve.New("lr_index.bleve", mapping)
		CheckError(err)
		err = index.Close()
		CheckError(err)
	}

	// Init Elasticsearch attachment
	if EnableES == "1" {
		// Elasticsearch settings
		type Attachment struct {
			Field        string `json:"field"`
			IndexedChars int64  `json:"indexed_chars"`
		}

		type Processors struct {
			Attachment Attachment `json:"attachment"`
		}

		type AttachmentStruct struct {
			Description string       `json:"description"`
			Processors  []Processors `json:"processors"`
		}

		attachment := &AttachmentStruct{
			Description: "Process documents",
			Processors: []Processors{
				Processors{
					Attachment: Attachment{
						Field:        "thedata",
						IndexedChars: -1,
					},
				},
			},
		}

		fmt.Println(attachment)

		b, err := json.Marshal(attachment)
		CheckError(err)
		fmt.Println(b)

		PutJSON(ESPath+"/_ingest/pipeline/attachment", b)

		type Settings struct {
			NumberOfShards   int64 `json:"number_of_shards"`
			NumberOfReplicas int64 `json:"number_of_replicas"`
		}

		type IndexStruct struct {
			Settings Settings `json:"settings"`
		}

		// Init Elasticsearch index
		index := &IndexStruct{
			Settings{
				NumberOfShards:   4,
				NumberOfReplicas: 0,
			},
		}

		b, err = json.Marshal(index)
		CheckError(err)
		fmt.Println(b)

		PutJSON(ESPath+"/lr_index", b)
	}

	// Initiate redis
	client := redis.NewClient(&redis.Options{
		Addr:     RedisPath,
		Password: "", // no password set
		DB:       0,  // use default DB
	})

	// Create upload directory if not exist
	uploadPath := "./uploads/img"
	if _, err := os.Stat(uploadPath); os.IsNotExist(err) {
		err = os.MkdirAll(uploadPath, 0755)
	}

	// Set database and redis environment
	env := &Env{db: db, RedisClient: client}
	// Router
	r.GET("/", env.GetHomePage)
	r.GET("/signin", env.GetSignIn)
	r.POST("/signin", env.PostSignIn)
	r.GET("/forgot-password", GetForgotPassword)
	r.POST("/forgot-password", env.PostForgotPassword)
	r.GET("/reset-password", env.GetResetPassword)
	r.POST("/reset-password", env.PostResetPassword)
	r.GET("/signup", env.GetSignUp)
	r.POST("/signup", env.PostSignUp)
	r.GET("/confirm-email", env.ConfirmEmail)
	r.GET("/new-token", env.SendNewToken)
	r.GET("/signout", GetSignOut)
	r.POST("/upload", env.UploadBook)
	r.GET("/book/:bookname", env.SendBook)
	r.GET("/get-book-metadata", env.GetBookMetaData)
	r.POST("/edit-book/:bookname", env.EditBook)
	r.GET("/delete-book/:bookname", env.DeleteBook)
	r.GET("/load-epub-fragment/:bookname/:type", env.SendEPUBFragment)
	r.GET("/load-epub-fragment-from-id/:bookname/:id", env.SendEPUBFragmentFromId)
	r.GET("/get-epub-current-page", env.GetEPUBCurrentPage)
	r.GET("/cover/:covername", SendBookCover)
	r.GET("/books/:pagination", env.GetPagination)
	r.GET("/autocomplete", env.GetAutocomplete)
	r.GET("/collections", env.GetCollections)
	r.GET("/add-collection", env.GetAddCollection)
	r.POST("/post-new-collection", env.PostNewCollection)
	r.GET("/collection/:id", env.GetCollection)
	r.GET("/delete-collection/:id", env.DeleteCollection)
	r.POST("/post-pdf-highlight", env.PostPDFHighlight)
	r.GET("/get-pdf-highlights", env.GetPDFHighlights)
	r.POST("/post-pdf-highlight-color", env.PostPDFHighlightColor)
	r.POST("/post-pdf-highlight-comment", env.PostPDFHighlightComment)
	r.POST("/delete-pdf-highlight", env.DeletePDFHighlight)
	r.POST("/save-epub-highlight", env.SaveEPUBHighlight)
	r.GET("/settings", env.GetSettings)
	r.POST("/post-settings", env.PostSettings)

	// Listen and serve
	port, err := strconv.Atoi(ServerPort)
	if err != nil {
		fmt.Println("Invalid port specified")
		os.Exit(1)
	}
	r.Run(fmt.Sprintf(":%d", port))
}

func CheckError(err error) {
	if err != nil {
		fmt.Println(err)
	}
}

func _GetEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

var myClient = &http.Client{Timeout: 10 * time.Second}

func GetJSON(url string, target interface{}) error {
	r, err := myClient.Get(url)
	CheckError(err)
	if r != nil {
		defer r.Body.Close()
		return json.NewDecoder(r.Body).Decode(target)
	}
	return nil
}

func PutJSON(url string, message []byte) {
	req, err := http.NewRequest("PUT", url, bytes.NewBuffer(message))
	CheckError(err)
	req.Header.Set("Content-Type", "application/json")
	res, err := myClient.Do(req)
	CheckError(err)
	content, err := ioutil.ReadAll(res.Body)
	CheckError(err)
	fmt.Println(string(content))
}

func PostJSON(url string, message []byte) {
	fmt.Println(url)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(message))
	CheckError(err)
	req.Header.Set("Content-Type", "application/json")
	res, err := myClient.Do(req)
	CheckError(err)
	content, err := ioutil.ReadAll(res.Body)
	CheckError(err)
	fmt.Println(string(content))
}

func _GetEmailFromSession(c *gin.Context) interface{} {
	session := sessions.Default(c)
	return session.Get("email")
}

func (e *Env) _GetUserId(email string) int64 {
	rows, err := e.db.Query("SELECT `id` FROM `user` WHERE `email` = ?", email)
	CheckError(err)

	var userId int64
	if rows.Next() {
		err := rows.Scan(&userId)
		CheckError(err)
	}
	rows.Close()

	return userId
}

func (e *Env) _GetBookInfo(fileName string) (int64, string, string) {
	rows, err := e.db.Query("SELECT `id`, `format`, `file_path` FROM `book` WHERE `filename` = ?", fileName)
	CheckError(err)

	var (
		bookId   int64
		format   string
		filePath string
	)
	if rows.Next() {
		err := rows.Scan(&bookId, &format, &filePath)
		CheckError(err)
	}
	rows.Close()

	return bookId, format, filePath
}

func (e *Env) _GetBookMetaData(fileName string) (string, string, string, string) {
	rows, err := e.db.Query("SELECT `title`, `author`, `cover`, `format` FROM `book` WHERE `filename` = ?", fileName)
	CheckError(err)

	var (
		title  string
		author string
		cover  string
		format string
	)
	if rows.Next() {
		err := rows.Scan(&title, &author, &cover, &format)
		CheckError(err)
	}
	rows.Close()

	return title, author, cover, format
}

func (e *Env) _CheckCurrentlyReading(bookId int64) int64 {
	rows, err := e.db.Query("SELECT `id` FROM `currently_reading` WHERE `book_id` = ?", bookId)
	CheckError(err)

	var currentlyReadingId int64
	if rows.Next() {
		err := rows.Scan(&currentlyReadingId)
		CheckError(err)
	}
	rows.Close()

	return currentlyReadingId
}

func (e *Env) _UpdateCurrentlyReading(currentlyReadingId int64, bookId int64, userId int64, dateRead string) {
	if currentlyReadingId == 0 {
		// Insert a new record
		stmt, err := e.db.Prepare("INSERT INTO `currently_reading` (book_id, user_id, date_read) VALUES (?, ?, ?)")
		CheckError(err)

		res, err := stmt.Exec(bookId, userId, dateRead)
		CheckError(err)

		id, err := res.LastInsertId()
		CheckError(err)

		fmt.Println(id)
	} else {
		// Update dateRead for the given currentlyReadingId
		stmt, err := e.db.Prepare("UPDATE `currently_reading` SET date_read=? WHERE id=?")
		CheckError(err)

		_, err = stmt.Exec(dateRead, currentlyReadingId)
		CheckError(err)
	}
}

func _GetCurrentTime() string {
	t := time.Now()
	return t.Format("20060102150405")
}

func _GetManifestId(idArray []string, hrefArray []string, idRef string, filePath string) string {
	for i, e := range idArray {
		if e == idRef {
			hrefPath := filePath + "/" + hrefArray[i]
			return hrefPath
		}
	}
	return ""
}

func (e *Env) SendBook(c *gin.Context) {
	email := _GetEmailFromSession(c)
	if email != nil {
		name := c.Param("bookname")

		// Get user id
		userId := e._GetUserId(email.(string))

		// Get book id
		bookId, format, packagePath := e._GetBookInfo(name)

		// Remove dot from ./uploads
		filePathSplit := strings.Split(packagePath, "./uploads")
		packagePath = "/uploads" + filePathSplit[1]

		var idRef, hrefPath string
		var currentPage, totalPages int64
		if format == "epub" {
			val, err := e.RedisClient.Get(name).Result()
			CheckError(err)

			opfMetadata := OPFMetadataStruct{}
			json.Unmarshal([]byte(val), &opfMetadata)

			val, err = e.RedisClient.Get(name + "...current_page...").Result()
			CheckError(err)

			currentPage, err = strconv.ParseInt(val, 10, 64)
			CheckError(err)

			val, err = e.RedisClient.Get(name + "...current_fragment...").Result()
			CheckError(err)

			idRefIndex, err := strconv.ParseInt(val, 10, 64)
			CheckError(err)

			idRef = opfMetadata.Spine.ItemRef.IdRef[idRefIndex]
			id := opfMetadata.Manifest.Item.Id
			href := opfMetadata.Manifest.Item.Href

			hrefPath = _GetManifestId(id, href, idRef, packagePath)

			val, err = e.RedisClient.Get(name + "...total_pages...").Result()
			CheckError(err)

			totalPages, err = strconv.ParseInt(val, 10, 64)
			CheckError(err)
		}

		// Get current time for date read to be used for currently reading feature
		dateRead := _GetCurrentTime()

		// Check if book already exists in currently_reading table
		currentlyReadingId := e._CheckCurrentlyReading(bookId)

		// Update currently_reading table
		e._UpdateCurrentlyReading(currentlyReadingId, bookId, userId, dateRead)

		if format == "pdf" {
			// Return viewer.html for PDF viewer
			c.HTML(200, "viewer.html", gin.H{
				"fileName": name,
			})
		} else {
			// Return epub file xhtml file path
			c.HTML(200, "epub_viewer.html", gin.H{
				"fileName":    name,
				"idRef":       idRef,
				"packagePath": packagePath,
				"filePath":    hrefPath,
				"currentPage": currentPage,
				"totalPages":  totalPages,
			})
		}
	}

	// if not signed in, redirect to sign in page
	c.Redirect(302, "/signin")
}

type GetBookMetadataStruct struct {
	Title  string `json:"title"`
	Author string `json:"author"`
	Cover  string `json:"cover"`
}

func (e *Env) GetBookMetaData(c *gin.Context) {
	q := c.Request.URL.Query()
	name := q["fileName"][0]

	// Get book metadata
	title, author, cover, format := e._GetBookMetaData(name)

	if format == "epub" {
		// Remove dot from cover
		cover = "/uploads" + strings.Split(cover, "./uploads")[1]
	}

	bookMetadata := GetBookMetadataStruct{
		Title:  title,
		Author: author,
		Cover:  cover,
	}

	c.JSON(200, bookMetadata)
}

type BookMetadataEditStruct struct {
	Doc BMESDoc `json:"doc"`
}

type BMESDoc struct {
	Title  string `json:"title"`
	Author string `json:"author"`
	Cover  string `json:"cover"`
}

func (e *Env) EditBook(c *gin.Context) {
	email := _GetEmailFromSession(c)
	if email != nil {
		userId := e._GetUserId(email.(string))

		fileName := c.PostForm("filename")
		fmt.Println(fileName)

		rows, err := e.db.Query("SELECT id, title, author, cover, url from book where filename=?", fileName)

		var (
			bookId  int64
			oTitle  string
			oAuthor string
			oCover  string
			oURL    string
		)

		for rows.Next() {
			err = rows.Scan(&bookId, &oTitle, &oAuthor, &oCover, &oURL)
			CheckError(err)
		}
		rows.Close()

		if EnableES == "0" {

			index, _ := bleve.Open("lr_index.bleve")
			indexId := strconv.Itoa(int(userId)) + "*****" + strconv.Itoa(int(bookId)) + "*****" + oTitle + "*****" + oAuthor + "*****" + oCover + "*****" + oURL + "*****"
			err = index.Delete(indexId)
			CheckError(err)

			err = index.Close()
			CheckError(err)
		}

		title := c.PostForm("title")
		fmt.Println(title)

		author := c.PostForm("author")
		fmt.Println(author)

		stmt, err := e.db.Prepare("update book set title=?, author=? where filename=?")
		CheckError(err)

		_, err = stmt.Exec(title, author, fileName)
		CheckError(err)

		file, _ := c.FormFile("cover")
		if file != nil {
			fmt.Println(file.Filename)
			c.SaveUploadedFile(file, "./uploads/img/"+file.Filename)

			oCover = "./uploads/img/" + file.Filename

			stmt, err := e.db.Prepare("update book set cover=? where filename=?")
			CheckError(err)

			_, err = stmt.Exec(oCover, fileName)
			CheckError(err)
		}

		if EnableES == "0" {
			message := struct {
				Id     string
				Title  string
				Author string
			}{
				Id:     strconv.Itoa(int(userId)) + "*****" + strconv.Itoa(int(bookId)) + "*****" + title + "*****" + author + "*****" + oCover + "*****" + oURL + "*****",
				Title:  title,
				Author: author,
			}

			index, _ := bleve.Open("lr_index.bleve")
			index.Index(message.Id, message)
			err = index.Close()
			CheckError(err)
		} else {
			bms := BookMetadataEditStruct{
				Doc: BMESDoc{
					Title:  title,
					Author: author,
					Cover:  oCover,
				},
			}

			fmt.Println(bms)

			indexURL := ESPath + "/lr_index/book_info/" + strconv.Itoa(int(userId)) + "_" + strconv.Itoa(int(bookId)) + "/_update"
			fmt.Println(indexURL)

			b, err := json.Marshal(bms)
			CheckError(err)

			PostJSON(indexURL, b)

			val, err := e.RedisClient.Get(fileName + "...total_pages...").Result()
			CheckError(err)

			totalPages, err := strconv.ParseInt(val, 10, 64)
			CheckError(err)

			for i := 0; i < int(totalPages); i++ {
				indexURL := ESPath + "/lr_index/book_detail/" + strconv.Itoa(int(userId)) + "_" + strconv.Itoa(int(bookId)) + "_" + strconv.Itoa(i) + "/_update"
				fmt.Println(indexURL)

				b, err := json.Marshal(bms)
				CheckError(err)

				PostJSON(indexURL, b)
			}
		}

		c.String(200, "Book metadata saved successfully")
	}
}

func DeleteHTTPRequest(url string) {
	req, err := http.NewRequest("DELETE", url, nil)
	CheckError(err)
	req.Header.Set("Content-Type", "application/json")
	res, err := myClient.Do(req)
	CheckError(err)

	// Write response.
	var bufferDelete bytes.Buffer
	res.Write(&bufferDelete)

	fmt.Println("--- DELETE RESPONSE ---")
	fmt.Println(string(bufferDelete.Bytes()))
}

func (e *Env) DeleteBook(c *gin.Context) {
	if os.Getenv("LIBREREAD_DEMO_SERVER") == "1" {
		c.String(200, "Deleting book is disabled in the demo server.")
	} else {
		email := _GetEmailFromSession(c)
		if email != nil {
			userId := e._GetUserId(email.(string))

			fileName := c.Param("bookname")
			fmt.Println(fileName)

			var bookId int64
			var title, author, cover, url string
			rows, err := e.db.Query("select id, title, author, cover, url from book where filename=?", fileName)
			CheckError(err)

			for rows.Next() {
				err = rows.Scan(&bookId, &title, &author, &cover, &url)
				CheckError(err)
			}
			rows.Close()

			stmt, err := e.db.Prepare("delete from book where filename=?")
			CheckError(err)

			_, err = stmt.Exec(fileName)
			CheckError(err)

			currentlyReadingId := e._CheckCurrentlyReading(bookId)

			if currentlyReadingId != 0 {
				stmt, err := e.db.Prepare("delete from currently_reading where book_id=?")
				CheckError(err)

				_, err = stmt.Exec(currentlyReadingId)
				CheckError(err)
			}

			if EnableES == "0" {
				index, _ := bleve.Open("lr_index.bleve")
				indexId := strconv.Itoa(int(userId)) + "*****" + strconv.Itoa(int(bookId)) + "*****" + title + "*****" + author + "*****" + cover + "*****" + url + "*****"
				err = index.Delete(indexId)
				CheckError(err)

				err = index.Close()
				CheckError(err)
			} else {
				indexURL := ESPath + "/lr_index/book_info/" + strconv.Itoa(int(userId)) + "_" + strconv.Itoa(int(bookId))
				fmt.Println(indexURL)

				DeleteHTTPRequest(indexURL)

				val, err := e.RedisClient.Get(fileName + "...total_pages...").Result()
				CheckError(err)

				totalPages, err := strconv.ParseInt(val, 10, 64)
				CheckError(err)

				for i := 0; i <= int(totalPages); i++ {
					indexURL := ESPath + "/lr_index/book_detail/" + strconv.Itoa(int(userId)) + "_" + strconv.Itoa(int(bookId)) + "_" + strconv.Itoa(i)
					fmt.Println(indexURL)

					DeleteHTTPRequest(indexURL)
				}
			}

			c.Redirect(302, "/")
		}
		c.Redirect(302, "/signin")
	}
}

type CurrentPageDataStruct struct {
	CurrentPage int64 `json:"current_page"`
	LeftNone    bool  `json:"left_none"`
	RightNone   bool  `json:"right_none"`
}

func (e *Env) GetEPUBCurrentPage(c *gin.Context) {
	q := c.Request.URL.Query()

	fileName := q["fileName"][0]
	currentFragment := q["pageChapter"][0]

	fmt.Println(currentFragment)

	val, err := e.RedisClient.Get(fileName).Result()
	CheckError(err)

	opfMetadata := OPFMetadataStruct{}
	json.Unmarshal([]byte(val), &opfMetadata)

	href := opfMetadata.Manifest.Item.Href
	id := opfMetadata.Manifest.Item.Id

	idRef := opfMetadata.Spine.ItemRef.IdRef

	var currentPage int64
	leftNone, rightNone := false, false
	for i, el := range href {
		if el == currentFragment {
			currentId := id[i]

			for j, f := range idRef {
				if f == currentId {
					currentPage = int64(j) + 1
					fmt.Println(currentPage)
					if currentPage-1 <= 0 {
						leftNone = true
					}
					if currentPage >= int64(len(idRef)) {
						rightNone = true
					}
				}
			}
		}
	}

	cpds := CurrentPageDataStruct{
		CurrentPage: currentPage,
		LeftNone:    leftNone,
		RightNone:   rightNone,
	}

	c.JSON(200, cpds)
}

type HrefDataStruct struct {
	CurrentPage int64  `json:"current_page"`
	HrefPath    string `json:"href_path"`
	LeftNone    bool   `json:"left_none"`
	RightNone   bool   `json:"right_none"`
}

func (e *Env) SendEPUBFragmentFromId(c *gin.Context) {
	email := _GetEmailFromSession(c)
	if email != nil {
		fileName := c.Param("bookname")
		gotoId, err := strconv.ParseInt(c.Param("id"), 10, 64)
		CheckError(err)

		fmt.Println(gotoId)

		packagePath, err := e.RedisClient.Get(fileName + "...filepath...").Result()
		CheckError(err)

		val, err := e.RedisClient.Get(fileName).Result()
		CheckError(err)

		opfMetadata := OPFMetadataStruct{}
		json.Unmarshal([]byte(val), &opfMetadata)

		href := opfMetadata.Manifest.Item.Href
		id := opfMetadata.Manifest.Item.Id

		idRef := opfMetadata.Spine.ItemRef.IdRef

		err = e.RedisClient.Set(fileName+"...current_page...", gotoId, 0).Err()
		CheckError(err)

		err = e.RedisClient.Set(fileName+"...current_fragment...", gotoId-1, 0).Err()
		CheckError(err)

		hrefPath := _GetManifestId(id, href, idRef[gotoId-1], packagePath)

		leftNone := false
		rightNone := false

		if gotoId-2 < 0 {
			leftNone = true
		}

		if gotoId >= int64(len(idRef)) {
			rightNone = true
		}

		hrefData := HrefDataStruct{
			CurrentPage: gotoId,
			HrefPath:    hrefPath,
			LeftNone:    leftNone,
			RightNone:   rightNone,
		}

		c.JSON(200, hrefData)
	} else {
		c.String(200, "Not signed in")
	}
}

func (e *Env) _GetEPUBFragment(fileName string, flowType string, packagePath string, currentFragment string, href []string, id []string, idRef []string) *HrefDataStruct {
	var hrefPath string
	leftNone := false
	rightNone := false

	var currentPage int64

	for i, el := range href {
		if el == currentFragment {
			currentId := id[i]

			for j, f := range idRef {
				if f == currentId {
					if flowType == "next" {
						nextIdRef := idRef[j+1]

						currentPage = (int64(j) + 1) + 1

						err := e.RedisClient.Set(fileName+"...current_page...", currentPage, 0).Err()
						CheckError(err)

						err = e.RedisClient.Set(fileName+"...current_fragment...", j+1, 0).Err()
						CheckError(err)

						fmt.Println("Next Fragment: " + nextIdRef)
						hrefPath = _GetManifestId(id, href, nextIdRef, packagePath)

						if j+2 >= len(idRef) {
							rightNone = true
						}
					} else {
						prevIdRef := idRef[j-1]

						currentPage = (int64(j) + 1) - 1

						err := e.RedisClient.Set(fileName+"...current_page...", currentPage, 0).Err()
						CheckError(err)

						err = e.RedisClient.Set(fileName+"...current_fragment...", j-1, 0).Err()
						CheckError(err)

						fmt.Println("Previous Fragment: " + prevIdRef)
						hrefPath = _GetManifestId(id, href, prevIdRef, packagePath)

						if j-2 < 0 {
							leftNone = true
						}
					}
					break
				}
			}
			break
		}
	}

	hrefData := HrefDataStruct{
		CurrentPage: currentPage,
		HrefPath:    hrefPath,
		LeftNone:    leftNone,
		RightNone:   rightNone,
	}

	return &hrefData
}

func (e *Env) SendEPUBFragment(c *gin.Context) {
	email := _GetEmailFromSession(c)
	if email != nil {
		fileName := c.Param("bookname")
		flowType := c.Param("type")
		fmt.Println(flowType)

		q := c.Request.URL.Query()
		hrefQuery := q["href"][0]

		// Remove '#' from the link
		hrefQuery = strings.Split(hrefQuery, "#")[0]

		packagePath, err := e.RedisClient.Get(fileName + "...filepath...").Result()
		CheckError(err)

		currentFragment := strings.Split(hrefQuery, packagePath+"/")[1]
		fmt.Println("Current Fragment: " + currentFragment)

		val, err := e.RedisClient.Get(fileName).Result()
		CheckError(err)

		opfMetadata := OPFMetadataStruct{}
		json.Unmarshal([]byte(val), &opfMetadata)

		href := opfMetadata.Manifest.Item.Href
		id := opfMetadata.Manifest.Item.Id

		idRef := opfMetadata.Spine.ItemRef.IdRef

		hrefData := e._GetEPUBFragment(fileName, flowType, packagePath, currentFragment, href, id, idRef)

		c.JSON(200, hrefData)

	} else {
		c.String(200, "Not signed in")
	}
}

func SendBookCover(c *gin.Context) {
	name := c.Param("covername")
	filePath := "./uploads/img/" + name

	c.File(filePath)
}

// Quotation Struct

type QuoteStruct struct {
	Author      string `json:"author" binding:"required"`
	AuthorURL   string `json:"authorURL" binding:"required"`
	FromBook    string `json:"fromBook" binding:"required"`
	FromBookURL string `json:"fromBookURL" binding:"required"`
	Image       string `json:"image" binding:"required"`
	Quote       string `json:"quote" binding:"required"`
}

// Book Struct

type BookStruct struct {
	Title string
	URL   string
	Cover string
}

type BookStructList []BookStruct

func (e *Env) _GetCurrentlyReadingBooks(userId int64) []int64 {
	rows, err := e.db.Query("SELECT `book_id` FROM `currently_reading` WHERE `user_id` = ? ORDER BY `date_read` DESC LIMIT ?, ?", userId, 0, 12)
	CheckError(err)

	var crBooks []int64
	for rows.Next() {
		var crBook int64
		err = rows.Scan(&crBook)
		CheckError(err)

		crBooks = append(crBooks, crBook)
	}
	rows.Close()

	return crBooks
}

func (e *Env) _GetBook(bookId int64) (string, string, string) {
	rows, err := e.db.Query("SELECT `title`, `url`, `cover` FROM `book` WHERE `id` = ?", bookId)
	CheckError(err)

	var (
		title string
		url   string
		cover string
	)

	if rows.Next() {
		err = rows.Scan(
			&title,
			&url,
			&cover,
		)
		CheckError(err)
	}
	rows.Close()

	return title, url, cover
}

func (e *Env) _GetTotalBooksCount(userId int64) int64 {
	rows, err := e.db.Query("SELECT COUNT(*) AS count FROM `book` WHERE `user_id` = ?", userId)
	CheckError(err)

	var count int64
	for rows.Next() {
		err = rows.Scan(&count)
		CheckError(err)
	}
	rows.Close()

	return count
}

func _GetTotalPages(booksCount int64) int64 {
	totalPagesFloat := float64(float64(booksCount) / 18.0)
	totalPagesDecimal := fmt.Sprintf("%.1f", totalPagesFloat)

	var totalPages int64
	if strings.Split(totalPagesDecimal, ".")[1] == "0" {
		totalPages = int64(totalPagesFloat)
	} else {
		totalPages = int64(totalPagesFloat) + 1
	}

	return totalPages
}

func (e *Env) _GetPaginatedBooks(userId int64, limit int64, offset int64) *BookStructList {
	rows, err := e.db.Query("SELECT `title`, `url`, `cover` FROM `book` WHERE `user_id` = ? ORDER BY `id` DESC LIMIT ? OFFSET ?", userId, limit, offset)
	CheckError(err)

	books := BookStructList{}

	var (
		title string
		url   string
		cover string
	)
	for rows.Next() {
		err = rows.Scan(
			&title,
			&url,
			&cover,
		)
		CheckError(err)

		books = append(books, BookStruct{
			title,
			url,
			cover,
		})
	}
	rows.Close()

	return &books
}

func _ConstructBooksWithCount(books *BookStructList, length int64) []BookStructList {
	booksList := []BookStructList{}
	var i, j int64
	for i = 0; i < int64(len(*books)); i += length {
		j = i + length
		for j > int64(len(*books)) {
			j -= 1
		}
		booksList = append(booksList, (*books)[i:j])
	}

	return booksList
}

func (e *Env) _ConstructBooksForPagination(userId int64, limit int64, offset int64) (int64, *BookStructList, []BookStructList, []BookStructList, []BookStructList) {
	// Check total number of rows in book table
	booksCount := e._GetTotalBooksCount(userId)

	// With Total Books count, Get Total pages required
	totalPages := _GetTotalPages(booksCount)

	// Get 18 books
	books := e._GetPaginatedBooks(userId, limit, offset)

	// Construct books of length 6 for large screen size
	booksList := _ConstructBooksWithCount(books, 6)

	// Construct books of length 3 for medium screen size
	booksListMedium := _ConstructBooksWithCount(books, 3)

	// Construct books of length 2 for small screen size
	booksListSmall := _ConstructBooksWithCount(books, 2)

	return totalPages, books, booksList, booksListMedium, booksListSmall
}

func (e *Env) GetHomePage(c *gin.Context) {
	// Get session from cookie. Check if email exists
	// show Home page else redirect to signin page.
	email := _GetEmailFromSession(c)
	if email != nil {
		q := QuoteStruct{}
		GetJSON("https://qotd.libreread.org/", &q)

		if q.Quote == "" {
			q.Quote = "So many things are possible just as long as you don't know they're impossible."
			q.Author = "Norton Juster"
			q.AuthorURL = "https://www.goodreads.com/author/show/214.Norton_Juster"
			q.Image = "https://images.gr-assets.com/authors/1201117378p5/214.jpg"
			q.FromBook = "The Phantom Tollbooth"
			q.FromBookURL = "https://www.goodreads.com/work/1782584"
		}

		userId := e._GetUserId(email.(string))

		// Get currently reading books.
		crBooks := e._GetCurrentlyReadingBooks(userId)

		// Get book title, url, cover for currently reading books.
		currentlyReadingBooks := BookStructList{}
		for _, bookId := range crBooks {
			title, url, cover := e._GetBook(bookId)
			currentlyReadingBooks = append(currentlyReadingBooks, BookStruct{
				title,
				url,
				cover,
			})
		}

		totalPages, books, booksList, booksListMedium, booksListSmall := e._ConstructBooksForPagination(userId, 18, 0)

		c.HTML(302, "index.html", gin.H{
			"q": q,
			"currentlyReadingBooks": currentlyReadingBooks,
			"booksList":             booksList,
			"booksListMedium":       booksListMedium,
			"booksListSmall":        booksListSmall,
			"booksListXtraSmall":    *books,
			"totalPages":            totalPages,
		})
	}
	c.Redirect(302, "/signin")
}

func (e *Env) GetPagination(c *gin.Context) {
	email := _GetEmailFromSession(c)
	if email != nil {
		pagination, err := strconv.Atoi(c.Param("pagination"))
		CheckError(err)

		userId := e._GetUserId(email.(string))

		offset := (int64(pagination) - 1) * 18

		totalPages, books, booksList, booksListMedium, booksListSmall := e._ConstructBooksForPagination(userId, 18, offset)

		c.HTML(302, "pagination.html", gin.H{
			"pagination":         pagination,
			"booksList":          booksList,
			"booksListMedium":    booksListMedium,
			"booksListSmall":     booksListSmall,
			"booksListXtraSmall": books,
			"totalPages":         totalPages,
		})
	}
	c.Redirect(302, "/signin")
}

func (e *Env) GetSignIn(c *gin.Context) {
	email := _GetEmailFromSession(c)
	if email != nil {
		c.Redirect(200, "/")
	}

	rows, err := e.db.Query("select email from user where id = ?", 1)
	CheckError(err)

	defer rows.Close()
	var cEmail string
	if rows.Next() {
		err := rows.Scan(&cEmail)
		CheckError(err)
	}
	fmt.Println(cEmail)

	enableSignUp := false
	if cEmail == "" {
		enableSignUp = true
	}

	demoLabel := false
	if os.Getenv("LIBREREAD_DEMO_SERVER") == "1" {
		demoLabel = true
	}

	c.HTML(200, "signin.html", gin.H{
		"enableSignUp": enableSignUp,
		"demoLabel":    demoLabel,
	})
}

func GetSignOut(c *gin.Context) {
	session := sessions.Default(c)
	session.Delete("email")
	session.Save()

	c.Redirect(302, "/")
}

func (e *Env) _GetHashedPassword(email string) []byte {
	rows, err := e.db.Query("select password_hash from user where email = ?", email)
	CheckError(err)

	var hashedPassword []byte

	defer rows.Close()
	if rows.Next() {
		err := rows.Scan(&hashedPassword)
		CheckError(err)
	}

	return hashedPassword
}

func _CompareHashAndPassword(hashedPassword []byte, password []byte) error {
	// Comparing the password with the hash
	err := bcrypt.CompareHashAndPassword(hashedPassword, password)
	return err
}

func (e *Env) PostSignIn(c *gin.Context) {
	email := c.PostForm("email")
	password := []byte(c.PostForm("password"))

	hashedPassword := e._GetHashedPassword(email)

	err := _CompareHashAndPassword(hashedPassword, password)

	// err nil means it is a match
	if err == nil {
		c.Redirect(302, "/")

		// Set cookie based session for signin
		session := sessions.Default(c)
		session.Set("email", email)
		session.Save()
	} else {
		c.HTML(302, "signin.html", "")
	}
}

func GetForgotPassword(c *gin.Context) {
	c.HTML(302, "forgot_password.html", "")
}

func (e *Env) PostForgotPassword(c *gin.Context) {
	email := c.PostForm("email")
	rows, err := e.db.Query("select id, name from user where email = ?", email)
	CheckError(err)

	var (
		userID int64
		name   string
	)
	if rows.Next() {
		err := rows.Scan(&userID, &name)
		CheckError(err)
	}
	rows.Close()

	if userID != 0 {
		token := RandSeq(40)

		stmt, err := e.db.Prepare("update user set forgot_password_token=? where id=?")
		CheckError(err)

		_, err = stmt.Exec(token, userID)
		CheckError(err)

		resetPasswordLink := os.Getenv("LIBREREAD_DOMAIN_ADDRESS") + "/reset-password?token=" + token
		subject := "LibreRead: Reset your password"
		message := "Hi " + name +
			",<br><br>Please reset your password by clicking this link<br>" +
			resetPasswordLink

		go _SendEmail(email, name, subject, message)

		c.HTML(302, "forgot_message.html", gin.H{
			"message": "Reset password link has been sent to your email address.",
		})
	} else {
		c.HTML(302, "forgot_message.html", gin.H{
			"message": "No email address registered in that name.",
		})
	}
}

func (e *Env) GetResetPassword(c *gin.Context) {
	token := c.Request.URL.Query()["token"][0]
	fmt.Println(token)
	rows, err := e.db.Query("select email from user where forgot_password_token = ?", token)
	CheckError(err)

	var email string
	defer rows.Close()
	if rows.Next() {
		err := rows.Scan(&email)
		CheckError(err)
	}
	fmt.Println(email)

	if email != "" {
		c.HTML(200, "reset_password.html", gin.H{
			"email": email,
		})
	} else {
		c.HTML(200, "forgot_message.html", gin.H{
			"message": "Your reset password link is invalid.",
		})
	}
}

func (e *Env) PostResetPassword(c *gin.Context) {
	email := c.PostForm("email")
	password := []byte(c.PostForm("password"))

	// Hashing the password with the default cost of 10
	hashedPassword, err := bcrypt.GenerateFromPassword(password, bcrypt.DefaultCost)
	CheckError(err)

	stmt, err := e.db.Prepare("update user set password_hash=?, forgot_password_token=? where email=?")
	CheckError(err)

	_, err = stmt.Exec(hashedPassword, "", email)
	CheckError(err)

	c.HTML(200, "forgot_message.html", gin.H{
		"message": "Your password has been successfully changed.",
	})
}

func (e *Env) GetSignUp(c *gin.Context) {
	var email string
	rows, err := e.db.Query("select email from user where id = ?", 1)
	CheckError(err)

	defer rows.Close()
	if rows.Next() {
		err := rows.Scan(&email)
		CheckError(err)
	}
	fmt.Println(email)

	if email != "" {
		c.Redirect(302, "/signin")
	} else {
		c.HTML(302, "signup.html", "")
	}
}

func (e *Env) PostSignUp(c *gin.Context) {
	name := c.PostForm("name")
	email := c.PostForm("email")
	password := []byte(c.PostForm("password"))

	// Hashing the password with the default cost of 10
	hashedPassword, err := bcrypt.GenerateFromPassword(password, bcrypt.DefaultCost)
	CheckError(err)

	stmt, err := e.db.Prepare("INSERT INTO user (name, email, password_hash) VALUES (?, ?, ?)")
	CheckError(err)

	res, err := stmt.Exec(name, email, hashedPassword)
	CheckError(err)

	id, err := res.LastInsertId()
	CheckError(err)

	go e._SendConfirmationEmail(int64(id), name, email)

	c.HTML(302, "confirm_email.html", "")

}

// For confirm email token
var letters = []rune("0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func RandSeq(n int64) string {
	rand.Seed(time.Now().UTC().UnixNano())
	b := make([]rune, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

func (e *Env) _FillConfirmTable(token string, dateGenerated string, dateExpires string, userId int64) {
	stmt, err := e.db.Prepare("INSERT INTO confirm (token, date_generated, date_expires, user_id) VALUES (?, ?, ?, ?)")
	CheckError(err)

	_, err = stmt.Exec(token, dateGenerated, dateExpires, userId)
	CheckError(err)
}

func _SendEmail(email string, name string, subject string, message string) {
	// Set home many CPU cores this function wants to use
	runtime.GOMAXPROCS(runtime.NumCPU())
	fmt.Println(runtime.NumCPU())

	m := gomail.NewMessage()
	m.SetHeader("From", os.Getenv("LIBREREAD_SMTP_ADDRESS"))
	m.SetHeader("To", email)
	m.SetHeader("Subject", subject)
	m.SetBody("text/html", message)

	smtp_server := os.Getenv("LIBREREAD_SMTP_SERVER")
	smtp_port, err := strconv.Atoi(os.Getenv("LIBREREAD_SMTP_PORT"))
	CheckError(err)
	smtp_address := os.Getenv("LIBREREAD_SMTP_ADDRESS")
	smtp_password := os.Getenv("LIBREREAD_SMTP_PASSWORD")

	d := gomail.NewDialer(smtp_server, smtp_port, smtp_address, smtp_password)

	// Send the confirmation email
	if err := d.DialAndSend(m); err != nil {
		fmt.Println("Email Error:")
		fmt.Println(err)
	}
}

func (e *Env) _SendConfirmationEmail(userId int64, name string, email string) {

	// Set home many CPU cores this function wants to use
	runtime.GOMAXPROCS(runtime.NumCPU())
	fmt.Println(runtime.NumCPU())

	token := RandSeq(40)

	dateGenerated := _GetCurrentTime()

	// Apply one month time for token expiry
	t := time.Now()
	dateExpires := t.AddDate(0, 1, 0).Format("20060102150405")

	e._FillConfirmTable(token, dateGenerated, dateExpires, userId)

	confirmEmailLink := os.Getenv("LIBREREAD_DOMAIN_ADDRESS") + "/confirm-email?token=" + token

	message := "Hi " + name +
		",<br><br>Please confirm your email by clicking this link<br>" +
		confirmEmailLink

	subject := "LibreRead Email Confirmation"

	_SendEmail(email, name, subject, message)
}

func (e *Env) _GetConfirmTableRecord(token string, c *gin.Context) (int64, string, int64) {
	// Get id from confirm table with the token got from url.
	rows, err := e.db.Query("select id, date_expires, user_id from confirm where token = ?", token)
	CheckError(err)

	var (
		id          int64
		dateExpires string
		userId      int64
	)

	if rows.Next() {
		err := rows.Scan(&id, &dateExpires, &userId)
		CheckError(err)

		fmt.Println(id)
		fmt.Println(dateExpires)
	} else {
		c.HTML(404, "invalid_token.html", "")
		return 0, "", 0
	}
	rows.Close()

	return id, dateExpires, userId
}

func (e *Env) _UpdateConfirmTable(currentDateTime string, used int64, id int64) {
	stmt, err := e.db.Prepare("update confirm set date_used=?, used=? where id=?")
	CheckError(err)

	_, err = stmt.Exec(currentDateTime, 1, id)
	CheckError(err)
}

func (e *Env) _SetUserConfirmed(confirmed int64, userId int64) {
	stmt, err := e.db.Prepare("update user set confirmed=? where id=?")
	CheckError(err)

	_, err = stmt.Exec(confirmed, userId)
	CheckError(err)
}

func (e *Env) ConfirmEmail(c *gin.Context) {
	token := c.Request.URL.Query()["token"][0]

	id, dateExpires, userId := e._GetConfirmTableRecord(token, c)

	if currentDateTime := _GetCurrentTime(); currentDateTime < dateExpires {
		e._UpdateConfirmTable(currentDateTime, 1, id)

		e._SetUserConfirmed(1, userId)

		c.HTML(302, "confirmed.html", gin.H{
			"id": userId,
		})
		return
	} else {
		c.HTML(302, "expired.html", gin.H{
			"id": userId,
		})
		return
	}
}

func (e *Env) _GetNameEmailFromUser(userId int64) (string, string) {
	rows, err := e.db.Query("select name, email from user where id = ?", userId)
	CheckError(err)

	var (
		name  string
		email string
	)

	if rows.Next() {
		err := rows.Scan(&name, &email)
		CheckError(err)
	}
	rows.Close()

	return name, email
}

func (e *Env) SendNewToken(c *gin.Context) {
	userId, err := strconv.Atoi(c.Request.URL.Query()["id"][0])
	CheckError(err)

	name, email := e._GetNameEmailFromUser(int64(userId))

	go e._SendConfirmationEmail(int64(userId), name, email)
}

func _ConstructFileNameForBook(fileName string, contentType string) string {
	if contentType == "application/pdf" {
		fileName = strings.Split(fileName, ".pdf")[0]
		fileName = strings.Join(strings.Split(fileName, " "), "_") + ".pdf"
	} else if contentType == "application/epub+zip" {
		fileName = strings.Split(fileName, ".epub")[0]
		fileName = strings.Join(strings.Split(fileName, " "), "_") + ".epub"
	}

	return fileName
}

func _HasPrefix(opSplit []string, content string) string {
	for _, element := range opSplit {
		if strings.HasPrefix(element, content) {
			return strings.Trim(strings.Split(element, ":")[1], " ")
		}
	}
	return ""
}

func _GetPDFInfo(filePath string) (string, string, string) {
	cmd := exec.Command("pdfinfo", filePath)

	var out bytes.Buffer
	cmd.Stdout = &out

	err := cmd.Run()
	CheckError(err)

	output := out.String()
	opSplit := strings.Split(output, "\n")

	// Get book title.
	title := _HasPrefix(opSplit, "Title")

	// Get author of the uploaded book.
	author := _HasPrefix(opSplit, "Author")

	// Get total number of pages.
	pages := _HasPrefix(opSplit, "Pages")

	return title, author, pages
}

func _GeneratePDFCover(fileName, filePath, coverPath string) string {
	cmd := exec.Command("pdfimages", "-p", "-png", "-f", "1", "-l", "2", filePath, coverPath)

	err := cmd.Run()
	CheckError(err)

	if _, err := os.Stat(coverPath + "-001-000.png"); err == nil {
		cover := "/cover/" + fileName + "-001-000.png"
		return cover
	}
	return ""
}

func (e *Env) _InsertBookRecord(
	title string,
	fileName string,
	filePath string,
	author string,
	url string,
	cover string,
	pagesInt int64,
	format string,
	uploadedOn string,
	userId int64,
) int64 {
	stmt, err := e.db.Prepare("INSERT INTO book (title, filename, file_path, author, url, cover, pages, format, uploaded_on, user_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
	CheckError(err)

	res, err := stmt.Exec(title, fileName, filePath, author, url, cover, pagesInt, format, uploadedOn, userId)
	CheckError(err)

	id, err := res.LastInsertId()
	CheckError(err)

	return id
}

type BookInfoStruct struct {
	Title  string `json:"title"`
	Author string `json:"author"`
	URL    string `json:"url"`
	Cover  string `json:"cover"`
}

func _PDFSeparate(path string, filePath string, wg *sync.WaitGroup) error {
	runtime.GOMAXPROCS(runtime.NumCPU())
	fmt.Println(runtime.NumCPU())
	cmd := exec.Command("pdfseparate", filePath, path+"/%d.pdf")

	err := cmd.Start()
	CheckError(err)
	err = cmd.Wait()
	wg.Done()
	return nil
}

func _ConstructPDFIndexURL(userId int64, bookId int64, i int64, pageJSON []byte) {
	indexURL := ESPath + "/lr_index/book_detail/" +
		strconv.Itoa(int(userId)) + "_" + strconv.Itoa(int(bookId)) +
		"_" + strconv.Itoa(int(i)) + "?pipeline=attachment"
	fmt.Println("Index URL: " + indexURL)
	PutJSON(indexURL, pageJSON)
}

type BookDataStruct struct {
	TheData string `json:"thedata"`
	Title   string `json:"title"`
	Author  string `json:"author"`
	URL     string `json:"url"`
	SeURL   string `json:"se_url"`
	Cover   string `json:"cover"`
	Page    int64  `json:"page"`
	Format  string `json:"format"`
}

func _LoopThroughSplittedPages(userId int64, bookId int64, pagesInt int64, splitPDFPath string, title string, author string, url string, cover string) {
	var i int64
	for i = 1; i < (pagesInt + 1); i += 1 {
		pagePath := splitPDFPath + "/" + strconv.Itoa(int(i)) + ".pdf"
		if _, err := os.Stat(pagePath); os.IsNotExist(err) {
			continue
		}
		data, err := ioutil.ReadFile(pagePath)
		CheckError(err)

		sEnc := base64.StdEncoding.EncodeToString([]byte(string(data)))

		bookDetail := BookDataStruct{
			TheData: sEnc,
			Title:   title,
			Author:  author,
			URL:     url,
			SeURL:   "",
			Cover:   cover,
			Page:    i,
			Format:  "pdf",
		}

		pageJSON, err := json.Marshal(bookDetail)
		CheckError(err)

		_ConstructPDFIndexURL(userId, bookId, i, pageJSON)
	}
}

func FeedPDFContent(filePath string, userId int64, bookId int64, title string, author string, url string, cover string, pagesInt int64) {
	// Set home many CPU cores this function wants to use.
	runtime.GOMAXPROCS(runtime.NumCPU())
	fmt.Println(runtime.NumCPU())

	timeNow := _GetCurrentTime()
	splitPDFPath := "./uploads/splitpdf_" + strconv.Itoa(int(userId)) + "_" + timeNow
	if _, err := os.Stat(splitPDFPath); os.IsNotExist(err) {
		os.Mkdir(splitPDFPath, 0700)
	}

	defer os.RemoveAll(splitPDFPath)

	var wg sync.WaitGroup
	wg.Add(1)
	go _PDFSeparate(splitPDFPath, filePath, &wg)
	wg.Wait()
	fmt.Println("wg done!")

	_LoopThroughSplittedPages(userId, bookId, pagesInt, splitPDFPath, title, author, url, cover)
}

func _EPUBUnzip(filePath string, fileName string) string {
	fileNameWithoutExtension := strings.Split(fileName, ".epub")[0]

	cmd := exec.Command("unzip", filePath, "-d", "uploads/"+fileNameWithoutExtension+"/")

	err := cmd.Start()
	CheckError(err)
	err = cmd.Wait()

	epubUnzipPath := "./uploads/" + fileNameWithoutExtension

	return epubUnzipPath
}

// struct for META-INF/container.xml

type XMLContainerStruct struct {
	RootFiles XMLRootFiles `xml:"rootfiles"`
}

type XMLRootFiles struct {
	RootFile XMLRootFile `xml:"rootfile"`
}

type XMLRootFile struct {
	FullPath string `xml:"full-path,attr"`
}

func _FetchOPFFilePath(epubUnzipPath string, containerXMLPath string) (string, string) {
	// Following code is for fetching OPF file path
	containerXMLContent, err := ioutil.ReadFile(containerXMLPath)
	CheckError(err)

	containerXMLUnmarshalled := XMLContainerStruct{}
	err = xml.Unmarshal(containerXMLContent, &containerXMLUnmarshalled)
	CheckError(err)

	rootFilePath := containerXMLUnmarshalled.RootFiles.RootFile.FullPath
	opfFilePath := epubUnzipPath + "/" + rootFilePath

	return rootFilePath, opfFilePath
}

func _ConvertOpfToXml(opfFilePath string) string {
	// Convert OPF file to XML and return the converted XML's file path
	opfXMLPath := strings.Split(opfFilePath, ".opf")[0] + ".xhtml"

	cmd := exec.Command("cp", opfFilePath, opfXMLPath)
	err := cmd.Start()
	CheckError(err)
	err = cmd.Wait()

	return opfXMLPath
}

// struct for package.xhtml derived from package.opf

type OPFMetadataStruct struct {
	Metadata OPFMetadata `xml:"metadata"`
	Spine    OPFSpine    `xml:"spine"`
	Manifest OPFManifest `xml:"manifest"`
}

type OPFMetadata struct {
	Title  string `xml:"title"`
	Author string `xml:"creator"`
}

type OPFSpine struct {
	ItemRef OPFItemRef `xml:"itemref"`
}

type OPFItemRef struct {
	IdRef []string `xml:"idref,attr"`
}

type OPFManifest struct {
	Item OPFItem `xml:"item"`
}

type OPFItem struct {
	Id        []string `xml:"id,attr"`
	Href      []string `xml:"href,attr"`
	MediaType []string `xml:"media-type,attr"`
}

func (opfMetadata *OPFMetadataStruct) _FetchEPUBMetadata(opfXMLPath string) {
	opfXMLContent, err := ioutil.ReadFile(opfXMLPath)
	CheckError(err)

	err = xml.Unmarshal(opfXMLContent, &opfMetadata)
	CheckError(err)
}

func _FetchEpubCoverPath(packagePath, coverFilePath string) string {
	coverXMLContent, err := ioutil.ReadFile(coverFilePath)
	CheckError(err)

	//Parse the XMLContent to grab just the img element
	strContent := string(coverXMLContent)
	imgLoc := strings.Index(strContent, "<img")
	prefixRem := strContent[imgLoc:]
	endImgLoc := strings.Index(prefixRem, "/>")
	//Move over by 2 to recover the '/>'
	trimmed := prefixRem[:endImgLoc+2]

	type ImgSrcStruct struct {
		Src string `xml:"src,attr"`
	}

	imgSrc := ImgSrcStruct{}
	err = xml.Unmarshal([]byte(trimmed), &imgSrc)

	coverPath := packagePath + "/" + imgSrc.Src

	return coverPath
}

func (opfMetadata *OPFMetadataStruct) _FetchEPUBCover(packagePath, opfFilePath string) string {
	coverIdRef := opfMetadata.Spine.ItemRef.IdRef[0]

	var coverPath string
	if strings.Contains(coverIdRef, "cover") {
		for i, e := range opfMetadata.Manifest.Item.Id {
			if e == coverIdRef {
				coverPath = opfMetadata.Manifest.Item.Href[i]
				break
			}
		}
		coverFilePath := packagePath + "/" + coverPath

		if strings.Contains(coverFilePath, "html") || strings.Contains(coverFilePath, "xhtml") || strings.Contains(coverFilePath, "xml") {
			coverPath = _FetchEpubCoverPath(packagePath, coverFilePath)
		} else {
			coverPath = coverFilePath
		}
	} else {
		var coverHref string
		for i, e := range opfMetadata.Manifest.Item.Id {
			if strings.Contains(e, "cover") {
				coverHref = opfMetadata.Manifest.Item.Href[i]
				break
			}
		}

		coverFilePath := packagePath + "/" + coverHref
		if strings.Contains(coverFilePath, "html") || strings.Contains(coverFilePath, "xhtml") || strings.Contains(coverFilePath, "xml") {
			coverPath = _FetchEpubCoverPath(packagePath, coverFilePath)
		} else {
			coverPath = coverFilePath
		}
	}

	return coverPath
}

func (opfMetadata *OPFMetadataStruct) _FeedEPUBContent(packagePath string, title string, author string, cover string, url string, userId int64, bookId int64) {
	// Set home many CPU cores this function wants to use.
	runtime.GOMAXPROCS(runtime.NumCPU())
	fmt.Println(runtime.NumCPU())

	idRef := opfMetadata.Spine.ItemRef.IdRef
	id := opfMetadata.Manifest.Item.Id

	for i, e := range idRef {
		for j, f := range id {
			if f == e {
				data, err := ioutil.ReadFile(packagePath + "/" + opfMetadata.Manifest.Item.Href[j])
				CheckError(err)

				sEnc := base64.StdEncoding.EncodeToString([]byte(string(data)))

				bookDetail := BookDataStruct{
					TheData: sEnc,
					Title:   title,
					Author:  author,
					URL:     url,
					SeURL:   opfMetadata.Manifest.Item.Href[j],
					Cover:   cover,
					Page:    int64(i),
					Format:  "epub",
				}

				pageJSON, err := json.Marshal(bookDetail)
				CheckError(err)

				indexURL := ESPath + "/lr_index/book_detail/" +
					strconv.Itoa(int(userId)) + "_" + strconv.Itoa(int(bookId)) +
					"_" + strconv.Itoa(int(i)) + "?pipeline=attachment"
				fmt.Println("Index URL: " + indexURL)
				PutJSON(indexURL, pageJSON)
			}
		}
	}
}

func (e *Env) UploadBook(c *gin.Context) {
	if os.Getenv("LIBREREAD_DEMO_SERVER") == "1" {
		c.String(403, "Upload is disabled in the demo server.")
	} else {
		email := _GetEmailFromSession(c)
		if email != nil {
			userId := e._GetUserId(email.(string))

			multipart, err := c.Request.MultipartReader()
			CheckError(err)

			for {
				mimePart, err := multipart.NextPart()

				if err == io.EOF {
					break
				}

				// Get filename and content type
				_, params, err := mime.ParseMediaType(mimePart.Header.Get("Content-Disposition"))
				CheckError(err)
				contentType, _, err := mime.ParseMediaType(mimePart.Header.Get("Content-Type"))
				CheckError(err)

				// Construct filename for the book uploaded
				fileName := _ConstructFileNameForBook(params["filename"], contentType)

				bookId, _, _ := e._GetBookInfo(fileName)

				if bookId != 0 {
					c.String(200, fileName+" already exists. ")
					continue
				}

				uploadedOn := _GetCurrentTime()

				filePath := "./uploads/" + fileName

				out, err := os.Create(filePath)
				CheckError(err)

				_, err = io.Copy(out, mimePart)
				CheckError(err)

				out.Close()

				if contentType == "application/pdf" {

					title, author, pages := _GetPDFInfo(filePath)

					if title == "" {
						title = fileName
					}

					if author == "" {
						author = "unknown"
					}

					fmt.Println("Book title: " + title)
					fmt.Println("Book author: " + author)
					fmt.Println("Total pages: " + pages)

					pagesInt, err := strconv.ParseInt(pages, 10, 64)
					CheckError(err)

					err = e.RedisClient.Set(fileName+"...total_pages...", pagesInt, 0).Err()
					CheckError(err)

					url := "/book/" + fileName
					fmt.Println("Book URL: " + url)

					coverPath := "./uploads/img/" + fileName

					cover := _GeneratePDFCover(fileName, filePath, coverPath)

					fmt.Println("Book cover: " + cover)

					// Insert new book in `book` table
					bookId := e._InsertBookRecord(title, fileName, filePath, author, url, cover, pagesInt, "pdf", uploadedOn, userId)
					fmt.Println(bookId)

					if EnableES == "0" {
						index, err := bleve.Open("lr_index.bleve")
						CheckError(err)

						message := struct {
							Id     string
							Title  string
							Author string
						}{
							Id:     strconv.Itoa(int(userId)) + "*****" + strconv.Itoa(int(bookId)) + "*****" + title + "*****" + author + "*****" + cover + "*****" + url + "*****",
							Title:  title,
							Author: author,
						}

						index.Index(message.Id, message)
						err = index.Close()
						CheckError(err)
					} else {
						// Feed book info to ES
						bookInfo := BookInfoStruct{
							Title:  title,
							Author: author,
							URL:    url,
							Cover:  cover,
						}

						fmt.Println(bookInfo)

						indexURL := ESPath + "/lr_index/book_info/" + strconv.Itoa(int(userId)) + "_" + strconv.Itoa(int(bookId))
						fmt.Println(indexURL)

						b, err := json.Marshal(bookInfo)
						CheckError(err)

						PutJSON(indexURL, b)

						// Feed book content to ES
						go FeedPDFContent(filePath, userId, bookId, title, author, url, cover, pagesInt)
					}

					c.String(200, fileName+" uploaded successfully. ")

				} else if contentType == "application/epub+zip" {
					// Unzip epub in the /uploads directory
					epubUnzipPath := _EPUBUnzip(filePath, fileName)
					containerXMLPath := epubUnzipPath + "/META-INF/container.xml"

					// Fetch rootfile and OPF file path
					rootFilePath, opfFilePath := _FetchOPFFilePath(epubUnzipPath, containerXMLPath)

					packagePath := epubUnzipPath
					rootFilePathSplit := strings.Split(rootFilePath, "/")
					if len(rootFilePathSplit) > 1 {
						packagePath = epubUnzipPath + "/" + rootFilePathSplit[0]
					}

					// Convert opf file to xml
					opfXMLPath := _ConvertOpfToXml(opfFilePath)

					opfMetadata := OPFMetadataStruct{}
					opfMetadata._FetchEPUBMetadata(opfXMLPath)

					title := opfMetadata.Metadata.Title
					author := opfMetadata.Metadata.Author
					cover := opfMetadata._FetchEPUBCover(packagePath, opfFilePath)

					fmt.Println("Book title: " + title)
					fmt.Println("Book author: " + author)

					// Store opfMetadata in Redis
					opfJSON, err := json.Marshal(opfMetadata)
					CheckError(err)

					err = e.RedisClient.Set(fileName, string(opfJSON), 0).Err()
					CheckError(err)

					totalPages := len(opfMetadata.Spine.ItemRef.IdRef)
					err = e.RedisClient.Set(fileName+"...total_pages...", totalPages, 0).Err()
					CheckError(err)

					err = e.RedisClient.Set(fileName+"...current_page...", 1, 0).Err()
					CheckError(err)

					err = e.RedisClient.Set(fileName+"...current_fragment...", 0, 0).Err()
					CheckError(err)

					// Remove dot from ./uploads
					packagePathSplit := strings.Split(packagePath, "./uploads")
					redisPackagePath := "/uploads" + packagePathSplit[1]

					err = e.RedisClient.Set(fileName+"...filepath...", redisPackagePath, 0).Err()
					CheckError(err)

					url := "/book/" + fileName

					// Insert new book in `book` table
					bookId := e._InsertBookRecord(title, fileName, packagePath, author, url, cover, 1, "epub", uploadedOn, userId)
					fmt.Println(bookId)

					if EnableES == "0" {
						index, err := bleve.Open("lr_index.bleve")
						CheckError(err)

						message := struct {
							Id     string
							Title  string
							Author string
						}{
							Id:     strconv.Itoa(int(userId)) + "*****" + strconv.Itoa(int(bookId)) + "*****" + title + "*****" + author + "*****" + cover + "*****" + url + "*****",
							Title:  title,
							Author: author,
						}

						index.Index(message.Id, message)
						err = index.Close()
						CheckError(err)
					} else {
						// Feed book info to ES
						bookInfo := BookInfoStruct{
							Title:  title,
							Author: author,
							URL:    url,
							Cover:  cover,
						}

						fmt.Println(bookInfo)

						// Feed book info to ES
						indexURL := ESPath + "/lr_index/book_info/" + strconv.Itoa(int(userId)) + "_" + strconv.Itoa(int(bookId))
						fmt.Println(indexURL)

						b, err := json.Marshal(bookInfo)
						CheckError(err)

						PutJSON(indexURL, b)

						// Feed book detail to ES
						go opfMetadata._FeedEPUBContent(packagePath, title, author, cover, url, userId, bookId)
					}

					c.String(200, fileName+" uploaded successfully. ")
				}
			}
		}
	}
}

// struct for marshalling book info

type BookInfoPayloadStruct struct {
	Source []string      `json:"_source"`
	Query  BookInfoQuery `json:"query"`
}

type BookInfoQuery struct {
	MultiMatch MultiMatchQuery `json:"multi_match"`
}

type MultiMatchQuery struct {
	Query  string   `json:"query"`
	Fields []string `json:"fields"`
}

// struct for marshalling book detail

type BookDetailPayloadStruct struct {
	Source    []string            `json:"_source"`
	Query     BookDetailQuery     `json:"query"`
	Highlight BookDetailHighlight `json:"highlight"`
}

type BookDetailQuery struct {
	MatchPhrase BookDetailMatchPhrase `json:"match_phrase"`
}

type BookDetailMatchPhrase struct {
	AttachmentContent string `json:"attachment.content"`
}

type BookDetailHighlight struct {
	Fields BookDetailHighlightFields `json:"fields"`
}

type BookDetailHighlightFields struct {
	AttachmentContent BookDetailHighlightAttachmentContent `json:"attachment.content"`
}

type BookDetailHighlightAttachmentContent struct {
	FragmentSize      int64 `json:"fragment_size"`
	NumberOfFragments int64 `json:"number_of_fragments"`
	NoMatchSize       int64 `json:"no_match_size"`
}

// struct for unmarshalling book info result

type BookInfoResultStruct struct {
	Hits BookInfoHits `json:"hits"`
}

type BookInfoHits struct {
	Hits []BookInfoHitsHits `json:"hits"`
}

type BookInfoHitsHits struct {
	Source BookInfoStruct `json:"_source"`
}

// struct for ummarshalling book detail result

type BookDetailResultStruct struct {
	Hits BookDetailHits `json:"hits"`
}

type BookDetailHits struct {
	Hits []BookDetailHitsHits `json:"hits"`
}

type BookDetailHitsHits struct {
	Source    BookDetailSource          `json:"_source"`
	Highlight BookDetailHighlightResult `json:"highlight"`
}

type BookDetailHighlightResult struct {
	AttachmentContent []string `json:"attachment.content"`
}

type BookDetailSource struct {
	Title  string `json:"title"`
	Author string `json:"author"`
	URL    string `json:"url"`
	SeURL  string `json:"se_url"`
	Cover  string `json:"cover"`
	Page   int64  `json:"page"`
	Format string `json:"format"`
}

// struct for wrapping book search result
type BookSearchResult struct {
	BookInfo   []BookInfoStruct     `json:"book_info"`
	BookDetail []BookDetailHitsHits `json:"book_detail"`
}

func GetJSONPassPayload(url string, payload []byte) []byte {
	req, err := http.NewRequest("GET", url, bytes.NewBuffer(payload))
	CheckError(err)
	req.Header.Set("Content-Type", "application/json")
	res, err := myClient.Do(req)
	CheckError(err)
	content, err := ioutil.ReadAll(res.Body)
	CheckError(err)
	fmt.Println(string(content))
	return content
}

func (e *Env) GetAutocomplete(c *gin.Context) {
	q := c.Request.URL.Query()
	term := q["term"][0]
	fmt.Println(term)

	email := _GetEmailFromSession(c)
	if email != nil {
		if EnableES == "0" {
			fmt.Println("Searching bleve ...")
			index, _ := bleve.Open("lr_index.bleve")
			// err = index.Delete("1_3")
			// CheckError(err)

			query := bleve.NewMatchQuery(term)
			search := bleve.NewSearchRequest(query)
			search.Highlight = bleve.NewHighlightWithStyle("html")
			search.Highlight.AddField("Title")
			search.Highlight.AddField("Author")
			searchResults, err := index.Search(search)
			CheckError(err)

			err = index.Close()
			CheckError(err)

			hitsBIS := []BookInfoStruct{}
			srSprint := fmt.Sprintf("%s", searchResults.Hits)
			if srSprint != "[]" {
				srSplit := strings.Split(srSprint, "] [")
				srSplit[0] = strings.Split(srSplit[0], "[[")[1]
				srSplit[len(srSplit)-1] = strings.Split(srSplit[len(srSplit)-1], "]]")[0]

				for _, el := range srSplit {
					result := strings.Split(el, "*****")
					fmt.Println(result)
					hitsBIS = append(hitsBIS, BookInfoStruct{
						Title:  result[2],
						Author: result[3],
						Cover:  result[4],
						URL:    result[5],
					})
				}
			}

			bsr := BookSearchResult{
				BookInfo:   hitsBIS,
				BookDetail: []BookDetailHitsHits{},
			}

			c.JSON(200, bsr)
		} else {
			fmt.Println("Searching elasticsearch ...")
			payloadInfo := &BookInfoPayloadStruct{
				Source: []string{"title", "author", "url", "cover"},
				Query: BookInfoQuery{
					MultiMatch: MultiMatchQuery{
						Query:  term,
						Fields: []string{"title", "author"},
					},
				},
			}

			b, err := json.Marshal(payloadInfo)
			CheckError(err)

			indexURL := ESPath + "/lr_index/book_info/_search"

			res := GetJSONPassPayload(indexURL, b)

			target := BookInfoResultStruct{}
			json.Unmarshal(res, &target)

			hits := target.Hits.Hits
			hitsBIS := []BookInfoStruct{}
			for _, el := range hits {
				hitsBIS = append(hitsBIS, BookInfoStruct{
					Title:  el.Source.Title,
					Author: el.Source.Author,
					URL:    el.Source.URL,
					Cover:  el.Source.Cover,
				})
			}

			payloadDetail := &BookDetailPayloadStruct{
				Source: []string{"title", "author", "url", "se_url", "cover", "page", "format"},
				Query: BookDetailQuery{
					MatchPhrase: BookDetailMatchPhrase{
						AttachmentContent: term,
					},
				},
				Highlight: BookDetailHighlight{
					Fields: BookDetailHighlightFields{
						AttachmentContent: BookDetailHighlightAttachmentContent{
							FragmentSize:      150,
							NumberOfFragments: 3,
							NoMatchSize:       150,
						},
					},
				},
			}
			b, err = json.Marshal(payloadDetail)
			CheckError(err)

			indexURL = ESPath + "/lr_index/book_detail/_search"

			res = GetJSONPassPayload(indexURL, b)

			target2 := BookDetailResultStruct{}
			json.Unmarshal(res, &target2)

			hits2 := target2.Hits.Hits
			hitsBDS := []BookDetailHitsHits{}
			for _, el := range hits2 {
				hitsBDS = append(hitsBDS, BookDetailHitsHits{
					Source:    el.Source,
					Highlight: el.Highlight,
				})
			}

			bsr := BookSearchResult{
				BookInfo:   hitsBIS,
				BookDetail: hitsBDS,
			}

			c.JSON(200, bsr)
		}
	}
}

type PDFHighlightStruct struct {
	PageIndex      []string `json:"pageIndex" binding:"required"`
	DivIndex       []string `json:"divIndex" binding:"required"`
	HTMLContent    []string `json:"htmlContent" binding:"required"`
	FileName       string   `json:"fileName" binding:"required"`
	HighlightColor string   `json:"highlightColor" binding:"required"`
}

func (e *Env) PostPDFHighlight(c *gin.Context) {
	email := _GetEmailFromSession(c)
	if email != nil {
		pdfHighlight := PDFHighlightStruct{}
		err := c.BindJSON(&pdfHighlight)
		CheckError(err)
		fmt.Println(pdfHighlight)

		// Get user id
		userId := e._GetUserId(email.(string))

		// Get book id
		bookId, _, _ := e._GetBookInfo(pdfHighlight.FileName)

		stmt, err := e.db.Prepare("INSERT INTO `pdf_highlighter` (book_id, user_id, highlight_color, highlight_top, highlight_comment) VALUES (?, ?, ?, ?, ?)")
		CheckError(err)

		res, err := stmt.Exec(bookId, userId, pdfHighlight.HighlightColor, "", "")
		CheckError(err)

		id, err := res.LastInsertId()
		CheckError(err)

		fmt.Println(id)

		for i := 0; i < len(pdfHighlight.DivIndex); i++ {
			stmt, err := e.db.Prepare("INSERT INTO `pdf_highlighter_detail` (highlighter_id, page_index, div_index, html_content) VALUES (?, ?, ?, ?)")
			CheckError(err)

			_, err = stmt.Exec(id, pdfHighlight.PageIndex[i], pdfHighlight.DivIndex[i], pdfHighlight.HTMLContent[i])
			CheckError(err)
		}

		c.String(200, strconv.Itoa(int(id)))
	} else {
		c.String(200, "Not signed in")
	}
}

type DeleteHighlightIdStruct struct {
	Id string `json:"id"`
}

func (e *Env) DeletePDFHighlight(c *gin.Context) {
	email := _GetEmailFromSession(c)
	if email != nil {
		deleteHighlight := DeleteHighlightIdStruct{}
		err := c.BindJSON(&deleteHighlight)
		CheckError(err)
		fmt.Println(deleteHighlight)

		// Delete highlight record with the given id
		stmt, err := e.db.Prepare("DELETE FROM `pdf_highlighter` WHERE id=?")
		CheckError(err)

		_, err = stmt.Exec(deleteHighlight.Id)
		CheckError(err)

		// Delete detail attached to the highlight
		stmt, err = e.db.Prepare("DELETE FROM `pdf_highlighter_detail` WHERE highlighter_id=?")
		CheckError(err)

		_, err = stmt.Exec(deleteHighlight.Id)

		c.String(200, "Highlight updated successfully")
	} else {
		c.String(200, "Not signed in")
	}
}

type GetPDFHighlightColorComment struct {
	Id               int64  `json:"id"`
	HighlightColor   string `json:"highlight_color"`
	HighlightTop     string `json:"highlight_top"`
	HighlightComment string `json:"highlight_comment"`
}

type GetPDFHighlightDetail struct {
	HId         int64  `json:"hid"`
	PageIndex   string `json:"page_index"`
	DivIndex    string `json:"div_index"`
	HTMLContent string `json:"html_content"`
}

type PDFHighlightsStruct struct {
	Color  []GetPDFHighlightColorComment `json:"color"`
	Detail []GetPDFHighlightDetail       `json:"detail"`
}

func (e *Env) GetPDFHighlights(c *gin.Context) {
	email := _GetEmailFromSession(c)
	if email != nil {
		q := c.Request.URL.Query()
		fileName := q["fileName"][0]
		fmt.Println(fileName)

		// Get user id
		userId := e._GetUserId(email.(string))

		// Get book id
		bookId, _, _ := e._GetBookInfo(fileName)

		rows, err := e.db.Query("select id, highlight_color, highlight_top, highlight_comment from pdf_highlighter where book_id = ? and user_id = ?", bookId, userId)
		CheckError(err)

		pdfHighlightColorComment := []GetPDFHighlightColorComment{}

		for rows.Next() {
			var (
				id               int64
				highlightColor   string
				highlightTop     string
				highlightComment string
			)

			err := rows.Scan(&id, &highlightColor, &highlightTop, &highlightComment)
			CheckError(err)

			pdfHighlightColorComment = append(pdfHighlightColorComment, GetPDFHighlightColorComment{
				Id:               id,
				HighlightColor:   highlightColor,
				HighlightTop:     highlightTop,
				HighlightComment: highlightComment,
			})
		}

		pdfHighlightDetail := []GetPDFHighlightDetail{}
		for _, v := range pdfHighlightColorComment {
			rows, err := e.db.Query("select page_index, div_index, html_content from pdf_highlighter_detail where highlighter_id=?", v.Id)
			CheckError(err)

			for rows.Next() {
				var (
					pageIndex   string
					divIndex    string
					htmlContent string
				)

				err := rows.Scan(&pageIndex, &divIndex, &htmlContent)
				CheckError(err)

				pdfHighlightDetail = append(pdfHighlightDetail, GetPDFHighlightDetail{
					HId:         v.Id,
					PageIndex:   pageIndex,
					DivIndex:    divIndex,
					HTMLContent: htmlContent,
				})
			}
		}

		pdfHighlights := PDFHighlightsStruct{
			Color:  pdfHighlightColorComment,
			Detail: pdfHighlightDetail,
		}

		c.JSON(200, pdfHighlights)
	} else {
		c.JSON(200, "Not signed in")
	}
}

type PDFHighlightColor struct {
	HighlightColor string `json:"highlightColor" binding:"required"`
	Id             string `json:"id" binding:"required"`
}

func (e *Env) PostPDFHighlightColor(c *gin.Context) {
	email := _GetEmailFromSession(c)
	if email != nil {
		pdfHighlightColor := PDFHighlightColor{}
		err := c.BindJSON(&pdfHighlightColor)
		CheckError(err)
		fmt.Println(pdfHighlightColor)

		// Update highlight color for the given id
		stmt, err := e.db.Prepare("UPDATE `pdf_highlighter` SET highlight_color=? WHERE id=?")
		CheckError(err)

		_, err = stmt.Exec(pdfHighlightColor.HighlightColor, pdfHighlightColor.Id)
		CheckError(err)

		c.String(200, "Highlight updated successfully")
	} else {
		c.String(200, "Not signed in")
	}
}

type PDFHighlightComment struct {
	Id      string `json:"id"`
	Top     string `json:"top"`
	Comment string `json:"comment"`
}

func (e *Env) PostPDFHighlightComment(c *gin.Context) {
	email := _GetEmailFromSession(c)
	if email != nil {
		pdfHighlightComment := PDFHighlightComment{}
		err := c.BindJSON(&pdfHighlightComment)
		CheckError(err)
		fmt.Println(pdfHighlightComment)

		// Update highlight comment for the given id
		stmt, err := e.db.Prepare("UPDATE `pdf_highlighter` SET highlight_top=?, highlight_comment=? WHERE id=?")
		CheckError(err)

		_, err = stmt.Exec(pdfHighlightComment.Top, pdfHighlightComment.Comment, pdfHighlightComment.Id)
		CheckError(err)

		c.String(200, "Highlight updated successfully")
	} else {
		c.String(200, "Not signed in")
	}
}

type EPUBHighlightStruct struct {
	FileName string `json:"fileName"`
	Href     string `json:"href"`
	HTML     string `json:"html"`
}

func (e *Env) SaveEPUBHighlight(c *gin.Context) {
	email := _GetEmailFromSession(c)
	if email != nil {
		epubHighlight := EPUBHighlightStruct{}
		err := c.BindJSON(&epubHighlight)
		CheckError(err)

		href := strings.Join(strings.Split(epubHighlight.Href, "/uploads"), "uploads")

		err = ioutil.WriteFile(href, []byte(epubHighlight.HTML), 0644)
		CheckError(err)

		c.String(200, "Highlight saved successfully")
	} else {
		c.String(200, "Not signed in")
	}
}

type CollectionBooks struct {
	Id          int64
	Title       string
	Description string
	Cover       string
}

func (e *Env) GetCollections(c *gin.Context) {
	email := _GetEmailFromSession(c)
	if email != nil {
		userId := e._GetUserId(email.(string))

		rows, err := e.db.Query("select id, title, description, cover from collection where user_id = ?", userId)
		CheckError(err)

		collectionBooks := []CollectionBooks{}
		for rows.Next() {
			var (
				id          int64
				title       string
				description string
				cover       sql.NullString
			)
			err := rows.Scan(&id, &title, &description, &cover)
			CheckError(err)

			var c string
			if cover.Valid {
				c = cover.String
			} else {
				c = ""
			}

			collectionBooks = append(collectionBooks, CollectionBooks{
				Id:          id,
				Title:       title,
				Description: description,
				Cover:       c,
			})
		}

		c.HTML(302, "collections.html", gin.H{
			"collectionBooks": collectionBooks,
		})
	} else {
		c.Redirect(302, "/signin")
	}
}

type BooksList struct {
	BookId int64
	Cover  string
}

func (e *Env) GetAddCollection(c *gin.Context) {
	email := _GetEmailFromSession(c)
	if email != nil {
		userId := e._GetUserId(email.(string))

		rows, err := e.db.Query("select id, cover from book where user_id = ?", userId)
		CheckError(err)

		books := []BooksList{}

		for rows.Next() {
			var (
				bookId int64
				cover  string
			)
			err := rows.Scan(&bookId, &cover)
			CheckError(err)

			books = append(books, BooksList{
				BookId: bookId,
				Cover:  cover,
			})
		}

		c.HTML(302, "add_collection.html", gin.H{
			"books": books,
		})
	} else {
		c.Redirect(302, "/signin")
	}
}

type PostCollection struct {
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Books       []int64 `json:"id"`
}

func (e *Env) PostNewCollection(c *gin.Context) {
	email := _GetEmailFromSession(c)
	if email != nil {
		userId := e._GetUserId(email.(string))

		postCollection := PostCollection{}
		err := c.BindJSON(&postCollection)
		CheckError(err)

		rows, err := e.db.Query("select cover from book where id = ?", postCollection.Books[len(postCollection.Books)-1])
		CheckError(err)

		var cover string
		if rows.Next() {
			err := rows.Scan(&cover)
			CheckError(err)
		}
		fmt.Println(cover)
		rows.Close()

		var books string
		for i, num := range postCollection.Books {
			if i == (len(postCollection.Books) - 1) {
				books += strconv.Itoa(int(num))
				break
			}
			books += strconv.Itoa(int(num)) + ","
		}
		fmt.Println(books)

		// -----------------------------------------------------
		// Fields: id, title, description, books, cover, user_id
		// -----------------------------------------------------
		stmt, err := e.db.Prepare("INSERT INTO `collection` (title, description, books, cover, user_id) VALUES (?, ?, ?, ?, ?)")
		CheckError(err)

		res, err := stmt.Exec(postCollection.Title, postCollection.Description, books, cover, userId)
		CheckError(err)

		id, err := res.LastInsertId()
		CheckError(err)

		c.String(200, strconv.Itoa(int(id)))
	} else {
		c.Redirect(302, "/signin")
	}
}

func (e *Env) GetCollection(c *gin.Context) {
	email := _GetEmailFromSession(c)
	if email != nil {
		collectionId := c.Param("id")

		rows, err := e.db.Query("select id, title, description, books from collection where id = ?", collectionId)
		CheckError(err)

		var (
			id          int64
			title       string
			description string
			cbooks      string
		)
		if rows.Next() {
			err := rows.Scan(&id, &title, &description, &cbooks)
			CheckError(err)
		}
		rows.Close()

		bookSplit := strings.Split(cbooks, ",")
		fmt.Println(bookSplit)

		b := BookStructList{}
		for i := len(bookSplit) - 1; i >= 0; i-- {
			bookInt, err := strconv.Atoi(bookSplit[i])
			CheckError(err)

			rows, err := e.db.Query("select title, url, cover from book where id = ?", bookInt)
			CheckError(err)

			if rows.Next() {
				var (
					title string
					url   string
					cover string
				)
				err := rows.Scan(&title, &url, &cover)
				CheckError(err)

				b = append(b, BookStruct{
					Title: title,
					URL:   url,
					Cover: cover,
				})
			}
			rows.Close()
		}

		// Construct books of length 6 for large screen size
		booksList := _ConstructBooksWithCount(&b, 6)

		// Construct books of length 3 for medium screen size
		booksListMedium := _ConstructBooksWithCount(&b, 3)

		// Construct books of length 2 for small screen size
		booksListSmall := _ConstructBooksWithCount(&b, 2)

		booksListXtraSmall := b

		c.HTML(302, "collection_item.html", gin.H{
			"id":                 id,
			"title":              title,
			"description":        description,
			"booksList":          booksList,
			"booksListMedium":    booksListMedium,
			"booksListSmall":     booksListSmall,
			"booksListXtraSmall": booksListXtraSmall,
		})
	} else {
		c.Redirect(302, "/signin")
	}
}

func (e *Env) DeleteCollection(c *gin.Context) {
	email := _GetEmailFromSession(c)
	if email != nil {
		collectionId := c.Param("id")

		stmt, err := e.db.Prepare("delete from collection where id=?")
		CheckError(err)

		_, err = stmt.Exec(collectionId)
		CheckError(err)

		c.Redirect(302, "/collections")
	} else {
		c.Redirect(302, "/signin")
	}
}

func (e *Env) GetSettings(c *gin.Context) {
	email := _GetEmailFromSession(c)
	if email != nil {
		c.HTML(302, "settings.html", gin.H{
			"email": email.(string),
		})
	} else {
		c.Redirect(302, "/signin")
	}
}

type PostSettingsStruct struct {
	Email          string `json:"email"`
	ChangePassword bool   `json:"change_password"`
	Password       string `json:"password"`
}

func (e *Env) PostSettings(c *gin.Context) {
	email := _GetEmailFromSession(c)
	if email != nil {
		if os.Getenv("LIBREREAD_DEMO_SERVER") == "1" {
			c.String(200, "You can't change settings in the demo server ;)")
		} else {
			postSettings := PostSettingsStruct{}
			err := c.BindJSON(&postSettings)
			CheckError(err)

			if postSettings.ChangePassword == true {
				// Hashing the password with the default cost of 10
				hashedPassword, err := bcrypt.GenerateFromPassword([]byte(postSettings.Password), bcrypt.DefaultCost)
				CheckError(err)

				stmt, err := e.db.Prepare("UPDATE `user` SET email=?, password_hash=? WHERE email=?")
				CheckError(err)

				_, err = stmt.Exec(postSettings.Email, hashedPassword, email.(string))
				CheckError(err)
			} else {
				stmt, err := e.db.Prepare("UPDATE `user` SET email=? WHERE email=?")
				CheckError(err)

				_, err = stmt.Exec(postSettings.Email, email.(string))
				CheckError(err)
			}

			c.String(200, "Successfully updated your settings.")
		}
	} else {
		c.Redirect(302, "/signin")
	}
}
