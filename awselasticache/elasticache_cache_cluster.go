package awselasticache

import (
	"errors"
	"fmt"
	"strings"

  "github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/elasticache"
	"github.com/pivotal-golang/lager"
)

type ElastiCacheCluster struct {
	region string
	iamsvc *iam.IAM
	cachesvc *elasticache.ElastiCache
	logger lager.Logger
}

func NewElastiCacheCluster(
	region string,
	iamsvc *iam.IAM,
	cachesvc *elasticache.ElastiCache,
	logger lager.Logger,
) *ElastiCacheCluster {
	return &ElastiCacheCluster{
		region: region,
		iamsvc: iamsvc,
		cachesvc: cachesvc,
		logger: logger.Session("elasticache-cluster"),
	}
}

func (r *ElastiCacheCluster) Describe(ID string) (CacheClusterDetails, error) {
	cacheClusterDetails := CacheClusterDetails{}
	input := &elasticache.DescribeCacheClustersInput{
		CacheClusterId: aws.String(ID),
		ShowCacheNodeInfo: aws.Bool(true),
	}

	r.logger.Debug("describe-cache-clusters", lager.Data{"input": input})
	cacheClusters, err := r.cachesvc.DescribeCacheClusters(input)
	if err != nil {
		r.logger.Error("aws-elasticache-error", err)
		if awsErr, ok := err.(awserr.Error); ok {
			if reqErr, ok := err.(awserr.RequestFailure); ok {
				if reqErr.StatusCode() == 404 {
					return cacheClusterDetails, ErrCacheClusterDoesNotExist
				}
			}
			return cacheClusterDetails, errors.New(awsErr.Code() + ": " + awsErr.Message())
		}
		return cacheClusterDetails, err
	}

	for _, cacheCluster := range cacheClusters.CacheClusters {
		if aws.StringValue(cacheCluster.CacheClusterId) == ID {
			r.logger.Debug("describe-cache-clusters", lager.Data{"cache-cluster": cacheCluster})
			return r.buildCacheCluster(cacheCluster), nil
		}
	}
	return cacheClusterDetails, ErrCacheClusterDoesNotExist
}


func (r *ElastiCacheCluster) Create(ID string, cacheClusterDetails CacheClusterDetails) error {
	input := r.buildCreateCacheClusterInput(ID, cacheClusterDetails)
	r.logger.Debug("create-cache-cluster", lager.Data{"input": input})

	output, err := r.cachesvc.CreateCacheCluster(input)
	if err != nil {
		r.logger.Error("aws-cache-error", err)
		if awsErr, ok := err.(awserr.Error); ok {
			return errors.New(awsErr.Code() + ": " + awsErr.Message())
		}
		return err
	}
	r.logger.Debug("create-cache-cluster", lager.Data{"output": output})

	return nil
}


func (r *ElastiCacheCluster) Modify(ID string, cacheClusterDetails CacheClusterDetails, applyImmediately bool) error {
	input := r.buildModifyCacheClusterInput(ID, cacheClusterDetails, applyImmediately)
	r.logger.Debug("modify-cache-cluster", lager.Data{"input": input})

	output, err := r.cachesvc.ModifyCacheCluster(input)
	if err != nil {
		r.logger.Error("aws-elasticache-error", err)
		if awsErr, ok := err.(awserr.Error); ok {
			if reqErr, ok := err.(awserr.RequestFailure); ok {
				if reqErr.StatusCode() == 404 {
					return ErrCacheClusterDoesNotExist
				}
			}
			return errors.New(awsErr.Code() + ": " + awsErr.Message())
		}
		return err
	}

	r.logger.Debug("modify-cache-cluster", lager.Data{"output": output})

	if len(cacheClusterDetails.Tags) > 0 {
		cacheClusterARN, err := r.cacheClusterARN(ID)
		if err != nil {
			return nil
		}

		tags := BuilElastiCacheTags(cacheClusterDetails.Tags)
		AddTagsToResource(cacheClusterARN, tags, r.cachesvc, r.logger)
	}

	return nil
}

func (r *ElastiCacheCluster) Delete(ID string) error {
	input := r.buildDeleteCacheClusterInput(ID)
	r.logger.Debug("delete-cache-cluster", lager.Data{"input": input})

	output, err := r.cachesvc.DeleteCacheCluster(input)
	if err != nil {
		r.logger.Error("aws-elasticache-error", err)
		if awsErr, ok := err.(awserr.Error); ok {
			if reqErr, ok := err.(awserr.RequestFailure); ok {
				if reqErr.StatusCode() == 404 {
					return ErrCacheClusterDoesNotExist
				}
			}
			return errors.New(awsErr.Code() + ": " + awsErr.Message())
		}
		return err
	}

	r.logger.Debug("delete-cache-cluster", lager.Data{"output": output})

	return nil
}

func UserAccount(iamsvc *iam.IAM) (string, error) {
	getUserInput := &iam.GetUserInput{}
	getUserOutput, err := iamsvc.GetUser(getUserInput)
	if err != nil {
		return "", err
	}

	userAccount := strings.Split(*getUserOutput.User.Arn, ":")

	return userAccount[4], nil
}

func (r *ElastiCacheCluster) cacheClusterARN(ID string) (string, error) {
	userAccount, err := UserAccount(r.iamsvc)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("arn:aws:elasticache:%s:%s:db:%s", r.region, userAccount, ID), nil
}

func (r *ElastiCacheCluster) buildCreateCacheClusterInput(ID string, cacheClusterDetails CacheClusterDetails) *elasticache.CreateCacheClusterInput {
	input := &elasticache.CreateCacheClusterInput{
		CacheClusterId: aws.String(ID),
		Engine:  aws.String(cacheClusterDetails.Engine),
	}

  if cacheClusterDetails.NumCacheNodes > 0 {
		input.NumCacheNodes = aws.Int64(cacheClusterDetails.NumCacheNodes)
	}
  if cacheClusterDetails.CacheInstanceClass != "" {
		input.CacheNodeType = aws.String(cacheClusterDetails.CacheInstanceClass)
	}
	if cacheClusterDetails.CacheSubnetGroupName != "" {
		input.CacheSubnetGroupName = aws.String(cacheClusterDetails.CacheSubnetGroupName)
	}

  if len(cacheClusterDetails.CacheSecurityGroups) > 0 {
		input.SecurityGroupIds = aws.StringSlice(cacheClusterDetails.CacheSecurityGroups)
	}

	if cacheClusterDetails.EngineVersion != "" {
		input.EngineVersion = aws.String(cacheClusterDetails.EngineVersion)
	}

	if cacheClusterDetails.Port > 0 {
		input.Port = aws.Int64(cacheClusterDetails.Port)
	}

	if len(cacheClusterDetails.Tags) > 0 {
		input.Tags = BuilElastiCacheTags(cacheClusterDetails.Tags)
	}

	return input
}


func (r *ElastiCacheCluster) buildDeleteCacheClusterInput(ID string) *elasticache.DeleteCacheClusterInput {
	input := &elasticache.DeleteCacheClusterInput{
		CacheClusterId: aws.String(ID),
	}
	return input
}

func (r *ElastiCacheCluster) buildModifyCacheClusterInput(ID string, cacheClusterDetails CacheClusterDetails, applyImmediately bool) *elasticache.ModifyCacheClusterInput {
	modifyDBClusterInput := &elasticache.ModifyCacheClusterInput{
		CacheClusterId: aws.String(ID),
		ApplyImmediately:    aws.Bool(applyImmediately),
	}

	return modifyDBClusterInput
}


func BuilElastiCacheTags(tags map[string]string) []*elasticache.Tag {
	var elasticacheTags []*elasticache.Tag

	for key, value := range tags {
		elasticacheTags = append(elasticacheTags, &elasticache.Tag{Key: aws.String(key), Value: aws.String(value)})
	}

	return elasticacheTags
}

func AddTagsToResource(resourceARN string, tags []*elasticache.Tag, cachesvc *elasticache.ElastiCache, logger lager.Logger) error {
	input := &elasticache.AddTagsToResourceInput{
		ResourceName: aws.String(resourceARN),
		Tags:         tags,
	}

	logger.Debug("add-tags-to-resource", lager.Data{"input": input})

	output, err := cachesvc.AddTagsToResource(input)
	if err != nil {
		logger.Error("aws-elasticache-error", err)
		if awsErr, ok := err.(awserr.Error); ok {
			return errors.New(awsErr.Code() + ": " + awsErr.Message())
		}
		return err
	}

	logger.Debug("add-tags-to-resource", lager.Data{"output": output})

	return nil
}

func (r *ElastiCacheCluster) buildCacheCluster(cacheCluster *elasticache.CacheCluster) CacheClusterDetails {
	cacheClusterDetails := CacheClusterDetails{
		CacheClusterId:   aws.StringValue(cacheCluster.CacheClusterId),
		Status:           aws.StringValue(cacheCluster.CacheClusterStatus),
		Engine:           aws.StringValue(cacheCluster.Engine),
		EngineVersion:    aws.StringValue(cacheCluster.EngineVersion),
		NumCacheNodes:    aws.Int64Value(cacheCluster.NumCacheNodes),
	}

	if len(cacheCluster.CacheNodes) > 0 {
		node := cacheCluster.CacheNodes[0]

		cacheClusterDetails.Endpoint = aws.StringValue(node.Endpoint.Address)
		cacheClusterDetails.Port = aws.Int64Value(node.Endpoint.Port)
	}

	return cacheClusterDetails
}
