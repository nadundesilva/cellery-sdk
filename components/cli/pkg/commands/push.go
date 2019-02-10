/*
 * Copyright (c) 2018 WSO2 Inc. (http:www.wso2.org) All Rights Reserved.
 *
 * WSO2 Inc. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http:www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

package commands

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"

	"github.com/docker/distribution/manifest"
	"github.com/docker/distribution/manifest/schema1"
	"github.com/docker/libtrust"
	"github.com/nokia/docker-registry-client/registry"
	"github.com/opencontainers/go-digest"

	"github.com/cellery-io/sdk/components/cli/pkg/constants"
	"github.com/cellery-io/sdk/components/cli/pkg/util"
)

// RunPush parses the cell image name to recognize the Cellery Registry (A Docker Registry), Organization and version
// and pushes to the Cellery Registry
func RunPush(cellImage string) error {
	err := pushImage(cellImage, "", "")
	if err != nil {
		fmt.Println()
		username, password, err := util.RequestCredentials()
		if err != nil {
			fmt.Printf("\x1b[31;1m Failed to acquire credentials: \x1b[0m %v \n", err)
			os.Exit(1)
		}
		fmt.Println()

		err = pushImage(cellImage, username, password)
		if err != nil {
			fmt.Printf("\x1b[31;1m Failed to push image: \x1b[0m %v \n", err)
			os.Exit(1)
		}
	}
	return nil
}

func pushImage(cellImage string, username string, password string) error {
	parsedCellImage, err := util.ParseImage(cellImage)
	if err != nil {
		fmt.Printf("\x1b[31;1m Error occurred while parsing cell image: \x1b[0m %v \n", err)
		os.Exit(1)
	}
	repository := parsedCellImage.Organization + "/" + parsedCellImage.ImageName

	spinner := util.StartNewSpinner("Pushing image " + util.Bold(cellImage))
	defer func() {
		spinner.IsSpinning = false
	}()

	// Initiating a connection to Cellery Registry
	fmt.Print(username)
	fmt.Print(password)
	hub, err := registry.New("https://"+parsedCellImage.Registry, username, password)
	if err != nil {
		fmt.Printf("\x1b[31;1m Error occurred while initializing connection to the Cellery Registry: "+
			"\x1b[0m %v \n", err)
		os.Exit(1)
	}

	// Reading the cell image
	cellImageFilePath := filepath.Join(util.UserHomeDir(), ".cellery", "repos", parsedCellImage.Registry,
		parsedCellImage.Organization, parsedCellImage.ImageName, parsedCellImage.ImageVersion,
		parsedCellImage.ImageName+constants.CELL_IMAGE_EXT)
	cellImageFile, err := os.Open(cellImageFilePath)
	if err != nil {
		fmt.Printf("\x1b[31;1m Error occurred while reading the cell image: \x1b[0m %v \n", err)
		os.Exit(1)
	}
	if cellImageFile != nil {
		defer func() {
			err := cellImageFile.Close()
			if err != nil {
				fmt.Printf("\x1b[31;1m Error occurred while opening the cell image: \x1b[0m %v \n", err)
				os.Exit(1)
			}
		}()
	}
	cellImageFileBytes, err := ioutil.ReadAll(cellImageFile)
	if err != nil {
		fmt.Printf("\x1b[31;1m Error occurred while reading the cell image: \x1b[0m %v \n", err)
		os.Exit(1)
	}

	// Creating the Cell Image Digest (Docker file Layer digest)
	hash := sha256.New()
	hash.Write(cellImageFileBytes)
	sha256sum := hex.EncodeToString(hash.Sum(nil))
	cellImageDigest := digest.NewDigestFromEncoded(digest.SHA256, sha256sum)

	// Checking if the the Cell Image already exists in the registry
	cellImageDigestExists, err := hub.HasBlob(repository, cellImageDigest)
	if err != nil {
		return err
	}

	// Pushing the cell image if it is not already uploaded
	if !cellImageDigestExists {
		// Read stream of files
		err = hub.UploadBlob(repository, cellImageDigest, bytes.NewReader(cellImageFileBytes), nil)
		if err != nil {
			return err
		}
		log.Printf("Successfully uploaded %s cell image", cellImage)
	} else {
		log.Printf("%s cell image already exists", cellImage)
	}

	// Creating a Docker manifest to be uploaded
	cellImageManifest := &schema1.Manifest{
		Name: repository,
		Versioned: manifest.Versioned{
			SchemaVersion: 1,
			MediaType:     schema1.MediaTypeSignedManifest,
		},
		Tag:          parsedCellImage.ImageVersion,
		Architecture: "amd64",
		FSLayers: []schema1.FSLayer{
			{BlobSum: cellImageDigest},
		},
		History: []schema1.History{
			{},
		},
	}

	// Signing the Docker Manifest
	key, err := libtrust.GenerateECP256PrivateKey()
	if err != nil {
		fmt.Printf("\x1b[31;1m Error occurred while pushing the cell image: \x1b[0m %v \n", err)
		os.Exit(1)
	}
	signedCellImageManifest, err := schema1.Sign(cellImageManifest, key)
	if err != nil {
		fmt.Printf("\x1b[31;1m Error occurred while pushing the cell image: \x1b[0m %v \n", err)
		os.Exit(1)
	}

	// Uploading the manifest to the Cellery Registry (Docker Registry)
	err = hub.PutManifest(repository, parsedCellImage.ImageVersion, signedCellImageManifest)
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("\nImage Digest : " + util.Bold(cellImageDigest))
	fmt.Printf(util.GreenBold("\U00002714")+" Successfully pushed cell image: %s\n", util.Bold(cellImage))
	util.PrintWhatsNextMessage("pull the image", "cellery pull "+cellImage)

	return nil
}
