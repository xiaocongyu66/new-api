// 推荐的壁纸查询接口：<方言>|<URL>，后端按方言解析 JSON 或直链。
// 顺序按实测响应速度排列：前四个支持关键词查询（动漫/SFW），后两个是图片直链兜底。
export const RECOMMENDED_WALLPAPER_APIS = [
  'gelbooru|https://safebooru.org/index.php?page=dapi&s=post&q=index&json=1&tags={keyword}+rating:general&limit=5',
  'e926|https://e926.net/posts.json?tags={keyword}&limit=5',
  'gelbooru|https://tbib.org/index.php?page=dapi&s=post&q=index&json=1&tags={keyword}+rating:general&limit=5',
  'wallhaven|https://wallhaven.clbug.com/api/v1/search?q={keyword}&categories=010&purity=100&sorting=random',
  'direct|https://t.alcy.cc/ycy/',
  'direct|https://img.xjh.me/random_img.php?type=json&return=302',
]

export const RECOMMENDED_WALLPAPER_API_TEXT = RECOMMENDED_WALLPAPER_APIS.join('\n')
