package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"sync"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/gin-gonic/gin"
	"github.com/unterstrich-kolkhoz/vid-thumbnailer/config"
	"github.com/satori/go.uuid"
)

var resizeMutex sync.Mutex

var gif = "./output.gif"

func resize(src string, w int, h int) (string, error) {
	resizeMutex.Lock()
	defer resizeMutex.Unlock()

	cmd := exec.Command("sh", "thmbnl.sh", src)
	err := cmd.Run()
	if err != nil {
		return "", err
	}

	f, err := ioutil.TempFile("", "thumbnailer")
	if err != nil {
		return "", err
	}
	defer f.Close()

	in, err := os.Open(gif)
	if err != nil {
		return "", err
	}
	defer in.Close()

	_, err = io.Copy(f, in)
	if err != nil {
		return "", err
	}

  err = f.Sync()
	if err != nil {
		return "", err
	}

	return f.Name(), nil
}

func getFile(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if code := resp.StatusCode; code != 200 {
		err = errors.New(fmt.Sprintf("Error getting the file '%s': HTTP %d",
			url, code))
		return "", err
	}

	f, err := ioutil.TempFile("", "thumbnailer")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return "", err
	}
	return f.Name(), nil
}

func upload(bucket string, region string, path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", errors.New(fmt.Sprintf("Unable to open file: %v", err))
	}
	defer file.Close()

	usr, err := user.Current()
	if err != nil {
		return "", err
	}

	dir := usr.HomeDir

	name, err := uuid.NewV4()

	if err != nil {
		return "", err
	}

	sess, err := session.NewSession(&aws.Config{
		Region:      aws.String(region),
		Credentials: credentials.NewSharedCredentials(dir+"/.aws/credentials", "thumbnailer"),
	})

	if err != nil {
		return "", err
	}

	uploader := s3manager.NewUploader(sess)

	info, err := uploader.Upload(&s3manager.UploadInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(name.String()),
		Body:   file,
	})
	if err != nil {
		return "", err
	}
	return info.Location, nil
}

type Body struct {
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Url    string `json:"url" binding:"required"`
}

func handleResize(bucket string, region string) func(c *gin.Context) {
	return func(c *gin.Context) {
		var body Body
		if err := c.ShouldBindJSON(&body); err != nil {
			c.String(http.StatusBadRequest, "Invalid body: ", err.Error())
			return
		}

		fname, err := getFile(body.Url)

		if err != nil {
			c.String(http.StatusBadRequest, err.Error())
			return
		}

		path, err := resize(fname, body.Width, body.Height)

		if err != nil {
			c.String(http.StatusBadRequest, err.Error())
			return
		}

		url, err := upload(bucket, region, path)

		if err != nil {
			log.Println(err.Error())
			c.String(http.StatusBadRequest, err.Error())
			return
		}

		c.JSON(http.StatusOK, gin.H{"url": url})
	}
}

func main() {
	configfile := flag.String("config", "./etc/vid-thumbnailer/server.conf", "Configuration file location")
	flag.Parse()
	conf, err := config.ReadConfig(*configfile)

	if err != nil {
		log.Fatal("Loading configuration failed: ", err)
	}

	r := gin.Default()

	r.POST("/", handleResize(conf.Bucket, conf.Region))
	r.Run(conf.Port)
}
