# HAMi

## git

```bash
git remote add upstream https://github.com/Project-HAMi/HAMi.git
git fetch upstream
git merge v2.6.1
```

## debug

```bash
docker pull registry.cn-qingdao.aliyuncs.com/wod/golang:1.24 && \
docker run -it --rm \
  -v $PWD:/go/src/github.com/Project-HAMi/HAMi \
  -w /go/src/github.com/Project-HAMi/HAMi \
  -e BUILD_VERSION=v2.6.1 \
  -u 1000:1000 \
  registry.cn-qingdao.aliyuncs.com/wod/golang:1.24 \
  bash .beagle/build.sh
```
