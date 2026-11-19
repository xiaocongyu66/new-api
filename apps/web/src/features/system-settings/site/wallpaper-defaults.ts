// 推荐的壁纸查询接口：<方言>|<URL>，后端按方言解析 JSON。
// Booru 模板带人物排除 tag，避免返回动漫人物插画；全部失败时回退到内置卡通图。
export const RECOMMENDED_WALLPAPER_APIS = [
  'gelbooru|https://safebooru.org/index.php?page=dapi&s=post&q=index&json=1&tags={keyword}+rating:general+-1girl+-1boy+-multiple_girls+-multiple_boys+-human+-cosplay+-person&limit=5',
  'e926|https://e926.net/posts.json?tags={keyword}+-1girl+-1boy+-multiple_girls+-multiple_boys+-human+-cosplay+-person&limit=5',
  'gelbooru|https://tbib.org/index.php?page=dapi&s=post&q=index&json=1&tags={keyword}+rating:general+-1girl+-1boy+-multiple_girls+-multiple_boys+-human+-cosplay+-person&limit=5',
  'wallhaven|https://wallhaven.clbug.com/api/v1/search?q={keyword}&categories=010&purity=100&sorting=random',
]

export const RECOMMENDED_WALLPAPER_API_TEXT = RECOMMENDED_WALLPAPER_APIS.join('\n')
