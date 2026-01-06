# HAMi

## git

```bash
git remote add upstream https://github.com/Project-HAMi/HAMi.git
git fetch upstream
git merge v2.6.1
```

## debug

```bash
# cache
docker run -it --rm \
  -v $PWD:/go/src/github.com/Project-HAMi/HAMi \
  -w /go/src/github.com/Project-HAMi/HAMi \
  -e BUILD_VERSION=v2.6.1 \
  -u 1000:1000 \
  registry.cn-qingdao.aliyuncs.com/wod/golang:1.24 \
  go mod vendor

# build
docker run -it --rm \
  -v $PWD:/go/src/github.com/Project-HAMi/HAMi \
  -w /go/src/github.com/Project-HAMi/HAMi \
  -e BUILD_VERSION=v2.6.1 \
  -u 1000:1000 \
  registry.cn-qingdao.aliyuncs.com/wod/golang:1.24 \
  bash .beagle/build.sh
```

## cache

```bash
# 构建缓存-->推送缓存至服务器
docker run --rm \
  -e PLUGIN_REBUILD=true \
  -e PLUGIN_ENDPOINT=$S3_ENDPOINT_ALIYUN \
  -e PLUGIN_ACCESS_KEY=$S3_ACCESS_KEY_ALIYUN \
  -e PLUGIN_SECRET_KEY=$S3_SECRET_KEY_ALIYUN \
  -e DRONE_REPO_OWNER="open-beagle" \
  -e DRONE_REPO_NAME="HAMI" \
  -e PLUGIN_MOUNT="./vendor" \
  -v $(pwd):$(pwd) \
  -w $(pwd) \
  registry.cn-qingdao.aliyuncs.com/wod/devops-s3-cache:1.0

# 读取缓存-->将缓存从服务器拉取到本地
docker run --rm \
  -e PLUGIN_RESTORE=true \
  -e PLUGIN_ENDPOINT=$S3_ENDPOINT_ALIYUN_ALIYUN \
  -e PLUGIN_ACCESS_KEY=$S3_ACCESS_KEY_ALIYUN \
  -e PLUGIN_SECRET_KEY=$S3_SECRET_KEY_ALIYUN \
  -e DRONE_REPO_OWNER="open-beagle" \
  -e DRONE_REPO_NAME="HAMI" \
  -v $(pwd):$(pwd) \
  -w $(pwd) \
  registry.cn-qingdao.aliyuncs.com/wod/devops-s3-cache:1.0
```
