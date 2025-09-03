## 代码来源
```
git remote add upstream  https://github.com/Project-HAMi/HAMi.git
git fetch upstream/release-v2.6
git merge upstream/release-v2.6

git log --pretty=oneline -3
git format-patch 33998613e36b60ee99c6fcb7a9315cbd0a258647 -1 --stdout > split-count.patch
git reset --hard a75f7315ffa1b03ed0512230624102e675af82b1
```