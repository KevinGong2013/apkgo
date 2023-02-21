# apkgo

apkgo帮助我们快速将apk包更新到各个平台。

## 快速开始

``` shell

brew install kevingong2013/tap/apkgo

```

> 无`brew`环境请前往[apk releases](https://github.com/KevinGong2013/apkgo/releases)下载对应系统的安装包(*支持windows*)。
> 
## 配置指南

目前支持的平台

- [华为](https://developer.huawei.com/consumer/cn/doc/development/AppGallery-connect-Guides/agcapi-getstarted-0000001111845114)
- [小米](https://dev.mi.com/distribute/doc/details?pId=1134)
- [vivo](https://dev.vivo.com.cn/documentCenter/doc/327)
- OPPO(vivo审核通过15分钟后会自动同步)
- [pyger](https://www.pgyer.com/doc/view/api#fastUploadApp)
- [fir.im](https://www.betaqr.com/docs/publish)
- [自定义内部服务](./docs/plugin.md)

> 请注意`apkgo`解决的是应用更新的问题，如果你是第一次发布，请前往对应商店的管理后台手动操作。

### 获取发布凭证

每个平台要求的鉴权凭证都不太一致，点击上面的链接前往各个平台获取对应的应用凭证，并将其配置在 `$HOME/.apkgo.json`中。

示例：

``` jsonc


{   
    // 需要上传的store鉴权凭证
    "stores": {
        "huawei": {
            "client_id": "1093143911913426",
            "client_secret": "D4F2F8A234232F59FD232CF5DEE03A29E"
        },
        "xiaomi": {
            "username": "example@icloud.com",
            "private_key": "e4p2302p2302p2302p2302pgessd1mrumvo3pzw"
        },
        "vivo": {
            "access_key": "20223420y0b",
            "access_secret": "01bss6dfsdfewfewf6aa4fb"
        },
        "pgyer": {
            "api_key": "5222323f90e0sdfewf5434sdsdssd"
        },
        "fir": {
            "api_token": "a090dsd59sdfsdfdsfc7e5"
        },
        "apkgo_demo": {
            "path": "/path/to/demo_plugin",
            "version": "23",
            "magic_cookie_key": "apkgo_demo_key",
            "magic_cookie_value": "apkgo_demo_value"
        }
    }
}

```

### 配置发布通知

`apkgo`支持`飞书`、`钉钉`、`企业微信`以及`自定义webhook`

``` jsonc

{
    "stores": {},

    // 配置你需要的通知即可
    "notifiers": {
        // https://open.feishu.cn/document/ukTMukTMukTM/ucTM5YjL3ETO24yNxkjN
        "lark": {
            "key": "14e7dfsc649-457e-aadfdaf4b2d",
            "secret_token": "9Ke0Rlsdfsadfsdwy40w4c"
        },
        "dingtalk": {
            // https://open.dingtalk.com/document/group/custom-robot-access
            "access_token": "fb1d40sfdsfdsfsdfsdfdsfdsfsdfdf8bf71377c3743adsfsdc2d6add60560",
            "secret_token": "SEC97db9dc553b6aasdfsdfsdfd0f56ff8sfdfsfdcff860"
        },
        // 微信群机器人
        "wecom": {
            "key": "7705e2d8-c44e-sfdsf8ae3cfsdfdsfd892"
        },
        "webhook": {
            // 将发布结果作为json body post到下面的url
            "url": [
                "https://central.rainbowbridge.top/api/apkgo/mock-webhook"
            ]
        }
    }
}

```

#### Example 1

``` shell

apkgo upload -f /path/to/release_flat_apk.go --store all

```

- `-f`: 将要上传的apk文件路径
- `-store all`: `all`指配置文件中的所有商店， 当然也可以指定某几个商店例如这样：`--store cams,xiaomi,huawei`

#### Example 2

``` shell

apkgo upload --file32 /path/to/release_apk_32.apk --file64 /path/to/release_apk_64.apk -s huawei,xiaomi --release-notes "1. 禁止了用户的微信登陆\n2. 禁止用户QQ登陆"

```

- `--file32`、`--file64`: 分包上传。 注意不是所有的商店都支持分包上传建议可以配合`--store`使用
- `--release-notes`: 更新日志

#### Example 3

``` shell

apkgo upload -f /path/to/apk.apk -s all --release-notes $CHANGELOG --disable-double-confirmation

```

- `--disable-double-confirmation`: 不二次确认直接开始发布

更多使用方式可以执行 `apkgo upload --help` 查看
