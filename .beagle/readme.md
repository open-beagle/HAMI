## 代码来源
```
git remote add upstream  https://github.com/Project-HAMi/HAMi.git
git fetch upstream/release-v2.6
git merge upstream/release-v2.6

git log --pretty=oneline -3
git format-patch 04d13927a82af63a81bfe28ad4673f14c2425805 -1 --stdout > .beagle/split-count-new.patch
git reset --hard 8578c28790a54588a49a79eb1868e8359d3c8fcc
```