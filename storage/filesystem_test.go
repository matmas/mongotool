package storage

import (
	"bytes"
	"fmt"
	. "github.com/smartystreets/goconvey/convey"
	"io"
	"io/ioutil"
	"os"
	"path"
	"testing"
)

var root = os.Getenv("TestRoot")

func TestFilesystem(t *testing.T) {
	const relative = "mongotooltest/object"
	defer os.Remove(path.Join(root, relative))

	Convey("Given an existing filesystem path", t, func() {
		if root == "" {
			SkipSo("TestRoot is not specified")
			return
		}
		store := Filesystem{root}
		Convey("Our storage implements the Saver interface", func() {
			saver := Saver(store)
			So(saver, ShouldNotBeNil)
		})
		Convey("We should be able to save an object using the relative path...", func() {
			r := bytes.NewReader([]byte("foo"))
			w, err := store.Save(relative)
			So(err, ShouldBeNil)
			_, err = io.Copy(w, r)
			So(err, ShouldBeNil)
			err = w.Close()
			So(err, ShouldBeNil)
			Convey("...Which should then have some bytes saved to it on the specified path", func() {
				So(path.Join(root, relative), shouldExistInFilesystem)
			})
		})
		Convey("Our storage implements the Fetcher interface", func() {
			fetcher := Fetcher(store)
			So(fetcher, ShouldNotBeNil)
		})
		Convey("We should be able to fetch the object we previously saved using the relative path...", func() {
			c, err := store.Fetch(relative)
			So(err, ShouldBeNil)

			objects := make([]io.ReadCloser, 0)
			for o := range c {
				objects = append(objects, o)
			}
			So(len(objects), ShouldEqual, 1)

			Convey("Which should contain what we saved", func() {
				b, err := ioutil.ReadAll(objects[0])
				So(err, ShouldBeNil)
				So(string(b), ShouldEqual, "foo")
				err = objects[0].Close()
				So(err, ShouldBeNil)
			})
		})
	})
}

func shouldExistInFilesystem(filename interface{}, _ ...interface{}) string {
	finfo, err := os.Stat(filename.(string))
	if err != nil {
		return fmt.Sprintf("Expected %s to exist but it did not.", filename)
	}
	if s := finfo.Size(); s == 0 {
		return fmt.Sprintf("Expected %s to have a filesize greater than 0 but it was %d", filename, s)
	}
	return ""
}
