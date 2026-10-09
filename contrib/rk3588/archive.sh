#!/usr/bin/env bash
# 交付归档集中在部署目录：运行配置、源码快照、测试记录和已验证镜像。
set -euo pipefail
root=$(git rev-parse --show-toplevel)
deploy=${DEPLOY_DIR:-/opt/DockerDate/bililive-go}
revision=$(git -C "$root" rev-parse HEAD)
stamp=$(date +%Y%m%d-%H%M%S)
archive_dir="$deploy/backups/rk3588-delivery-$stamp"
mkdir -p "$archive_dir/deploy" "$archive_dir/docs" "$archive_dir/source"
chmod 700 "$archive_dir"
cp -a "$deploy/config.yml" "$deploy/docker-compose.yml" "$deploy/.env" "$deploy/manage.sh" "$archive_dir/deploy/"
cp -a "$deploy/docs/." "$archive_dir/docs/"
# 用户可清理验证素材；归档只保存部署定义，不打包运行数据库、工具缓存或录像。
if test -d "$deploy/verification"; then
    mkdir -p "$archive_dir/verification"
    cp -a "$deploy/verification/." "$archive_dir/verification/"
fi
if test -f "$deploy/README.md"; then cp -a "$deploy/README.md" "$archive_dir/deploy/"; fi
cp "$root/contrib/rk3588/restore.sh" "$archive_dir/restore.sh"
printf '%s\n' "$revision" > "$archive_dir/source/base-revision.txt"
git -C "$root" diff --binary HEAD > "$archive_dir/source/changes.patch"
# Git 已跟踪源码和新增文件都保留，排除依赖、构建产物和临时调试文件。
git -C "$root" ls-files --cached --others --exclude-standard -z | tar -C "$root" --null -T - -czf "$archive_dir/source/source-tree.tar.gz"
docker save bililive-go-rk3588:20261009 bililive-rk3588-media:20261009 -o "$archive_dir/images.tar"
node - "$archive_dir" <<'NODE'
const fs=require('fs'), path=require('path'), crypto=require('crypto');
const root=process.argv[2], rows=[];
async function visit(dir) {
  for (const entry of fs.readdirSync(dir,{withFileTypes:true}).sort((a,b)=>a.name.localeCompare(b.name))) {
    const full=path.join(dir,entry.name);
    if(entry.isDirectory()) await visit(full);
    else if(entry.isFile() && entry.name!=='SHA256SUMS') {
      const hash=crypto.createHash('sha256');
      for await(const chunk of fs.createReadStream(full)) hash.update(chunk);
      rows.push(hash.digest('hex')+'  '+path.relative(root,full).split(path.sep).join('/'));
    }
  }
}
visit(root).then(()=>fs.writeFileSync(path.join(root,'SHA256SUMS'),rows.join('\n')+'\n')).catch(e=>{console.error(e);process.exitCode=1;});
NODE
(cd "$archive_dir" && sha256sum -c SHA256SUMS)
# 镜像层本身已压缩，低压缩级别避免归档占用过多 CPU。
tar -C "$deploy/backups" -cf - "$(basename "$archive_dir")" | gzip -1 > "$archive_dir.tar.gz"
sha256sum "$archive_dir.tar.gz" > "$archive_dir.tar.gz.sha256"
printf '交付归档：%s\n' "$archive_dir.tar.gz"
