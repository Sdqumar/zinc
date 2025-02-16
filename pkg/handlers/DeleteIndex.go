package handlers

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/blugelabs/bluge"
	"github.com/gin-gonic/gin"
	"github.com/prabhatsharma/zinc/pkg/core"
	"github.com/prabhatsharma/zinc/pkg/zutils"
	"github.com/rs/zerolog/log"
)

// DeleteIndex deletes a zinc index and its associated data. Be careful using thus as you ca't undo this action.
func DeleteIndex(c *gin.Context) {
	indexName := c.Param("indexName")

	// 0. Check if index exists and Get the index storage type - disk, s3 or memory
	indexExusts, indexStorageType := core.IndexExists(indexName)

	if !indexExusts {
		c.JSON(http.StatusBadRequest, gin.H{"error": "index " + indexName + "does not exist"})
		return
	}

	// 1. Close the index writer
	core.ZINC_INDEX_LIST[indexName].Writer.Close()

	// 2. Delete from the cache
	delete(core.ZINC_INDEX_LIST, indexName)

	// 3. Physically delete the index
	deleteIndexMapping := false
	if indexStorageType == "disk" {
		DATA_PATH := zutils.GetEnv("DATA_PATH", "./data")

		err := os.RemoveAll(DATA_PATH + "/" + indexName)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": err.Error(),
			})
			return
		} else {
			deleteIndexMapping = true
		}
	} else if indexStorageType == "s3" {

		err := deleteFilesForIndexFromS3(indexName)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": err.Error(),
			})
			log.Print("failed to delete index: ", err.Error())
		} else {
			deleteIndexMapping = true
		}
	}

	if deleteIndexMapping {
		// 4. Delete the index mapping
		bdoc := bluge.NewDocument(indexName)
		err := core.ZINC_SYSTEM_INDEX_LIST["_index_mapping"].Writer.Delete(bdoc.ID())

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": err.Error(),
			})
		} else {
			c.JSON(http.StatusOK, gin.H{
				"message": "Deleted",
				"index":   indexName,
				"storage": indexStorageType,
			})
		}
	}
}

func deleteFilesForIndexFromS3(indexName string) error {
	// Load the Shared AWS Configuration (~/.aws/config)
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		log.Print("Error loading AWS config: ", err)
		return err
	}
	client := s3.NewFromConfig(cfg)

	S3_BUCKET := zutils.GetEnv("S3_BUCKET", "")
	ctx := context.Background()

	// List Objects in the bucket at prefix
	listObjectsInput := &s3.ListObjectsV2Input{
		Bucket: &S3_BUCKET,
		Prefix: &indexName,
	}
	listObjectsOutput, err := client.ListObjectsV2(ctx, listObjectsInput)
	if err != nil {
		log.Print("failed to list objects: ", err.Error())
		return err
	}

	var fileList []types.ObjectIdentifier

	for _, object := range listObjectsOutput.Contents {
		fileList = append(fileList, types.ObjectIdentifier{
			Key: object.Key,
		})
		fmt.Println("Deleting: ", *object.Key)
	}

	doi := &s3.DeleteObjectsInput{
		Bucket: &S3_BUCKET,
		Delete: &types.Delete{
			Objects: fileList,
		},
	}
	_, err = client.DeleteObjects(ctx, doi)

	if err != nil {
		log.Print("failed to delete index: ", err.Error())
		return err
	}

	return nil
}
