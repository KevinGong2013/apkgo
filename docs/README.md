# apkgo

快速将安卓应用更新发布到主流应用商店

## 入门

### 安装

apkgo使用Go语言开发，几乎支持所有的常见平台。

#### 包管理器安装

[Homebrew](https://brew.sh) is a free and open source package manager for macOS and Linux. This will install apkgo:

``` shell
brew tap kevingong2013/tap
brew install apkgo
```

#### 预编译版

常见系统均可以前往[Github Release](https://github.com/KevinGong2013/apkgo/releases),下载安装包进行安装。

*windows*推荐使用此方法安装

#### Docker

``` shell
docker pull kevingong2013/apkgo
```

#### Build from source

To build apkgo from source you must:

1. install[Git](https://git-scm.com/book/en/v2/Getting-Started-Installing-Git)
2. Install[Go](https://go.dev/doc/install) version 1.21 or later

> The install directory is controlled by the GOPATH and GOBIN environment variables. If GOBIN is set, binaries are installed to that directory. If GOPATH is set, binaries are installed to the bin subdirectory of the first directory in the GOPATH list. Otherwise, binaries are installed to the bin subdirectory of the default GOPATH ($HOME/go or %USERPROFILE%\go).

Then build and test:

``` shell
go install github.com/KevinGong2013/apkgo@latest
apkgo --help

```

### 配置

#### 1.环境变量

apkgo使用`APKGO_HOME`环境变量来确认缓存文件位置，如不设置默认为`$HOME/.apkgo`，开始之前请确认`APKGO_HOME`

``` shell
#若输出为空则默认为 $HOME/.apkgo
echo $APKGO_HOME
```

> 本文档关于路径，如不特殊说明则全部是`$APKGO_HOME`的相对路径

#### 2. 初始化

apkgo支持两种模式

- 单机（即只在初始化的机器上使用）
- 团队（一人维护认证信息，其他成员皆可发布，也可与CI/CD及其他工具配合使用）

!> 团队模式下使用`git repo`同步各商店认证信息，注意一定要创建私有的git仓库并合理的配置仓库读写权限。

``` shell
# 单机
apkgo init --local
```

或者

``` shell
# 根据提示提供对应信息即可
apkgo init
```

#### 3. 认证信息

打开初始配置文件`$APKGO_HOME/secrets/store_config.json`，可以看到`stores`节点下包含了所有支持的商店。

所有商店的认证信息分为两类：

- ##### API鉴权

<!-- tabs:start -->

#### **华为AppGalleryConnect**

前往[华为AppGalleryConnect](https://developer.huawei.com/consumer/cn/doc/development/AppGallery-connect-Guides/agcapi-getstarted-0000001111845114)获取到`client_id`、`client_secret`。大概类似于下面这样：

``` json
{
    "huawei": {
        "client_id": "1093143911913426",
        "client_secret": "D4F2F8A234232F59FD232CF5DEE03A29E"
    }
}
```

#### **小米开放平台**

在[小米开放平台管理中心](https://dev.mi.com/distribute/doc/details?pId=1134)获取到`private_key`。大概类似于下面这样：

```json
{
    "xiaomi": {
        "username": "example@icloud.com",
        "private_key": "e4p2302p2302p2302p2302pgessd1mrumvo3pzw"
    }
}
```

> username 用户名，在小米开发者站登陆的邮箱

#### **vivo应用商店**

前往[vivo](https://dev.vivo.com.cn/documentCenter/doc/326)申请开通api传包服务，然后获取`access_key`、`access_secret`。大概类似于下面这样：

``` json
{
    "vivo": {
        "access_key": "20223420y0b",
        "access_secret": "01bss6dfsdfewfewf6aa4fb"
    }
}
```

#### **fir.im**

前往[文档](https://www.betaqr.com/docs)获取`api_token`。大概类似于下面这样：

```json
{
    "fir": {
        "api_token": "a090dsd59sdfsdfdsfc7e5"
    }
}
```

#### **蒲公英**

前往[文档](https://www.pgyer.com/doc/view/api#fastUploadApp)获取`api_key`。大概类似于下面这样：

```json
{
    "pgyer": {
        "api_key": "5222323f90e0sdfewf5434sdsdssd"
    }
}
```

<!-- tabs:end -->

!> 若不需要某个商店，直接删除掉对应的节点即可

配置完成自己需要的商店以后执行

``` shell
# 会检查所有配置的认证信息是否有效
# 如果全部有效且是团队使用则会将配置信息同步至git repo
apkgo check
```

- ##### 浏览器登陆

`oppo`,`tencent`,`baidu`,`qh360`均使用这种认证方式

``` shell
# 执行以下命令如果，oppo市场认证信息无效则会打开oppo管理后台
# 在管理后台登录成功以后会自动关闭浏览器同步认证信息
apkgo check oppo
```

- ##### 内部服务

内部分发服务的支持是通过[apkgo插件](/docs/advance/plugin/)实现。

###### 1. 下载插件

联系插件开发者获取插件，将插件放在任意位置
> 推荐放在`APKGO_HOME`根目录下

``` shell
# 直接执行你下载的插件
./apkgo_plugin

# 确保输出以下日志，证明插件安装成功
This binary is a plugin. These are not meant to be executed directly.
Please execute the program that consumes these plugins, which will
load any plugins automatically

```

###### 2. 配置插件

打开`APKGO_HOME/config.yaml`文件，添加如下配置

``` yaml
# 示例忽略掉
storage:
    location: local
# 主要是以下节点的配置
plugins:
      # apkgo 插件名称 联系插件开发者
    - name: apkgo-plugin
      # 插件在本地的路径
      path: path/to/your/apk-plugin
```

###### 3. 验证

```shell
apkgo check apkgo-plugin
```

### 发布

![v](https://img.shields.io/docker/v/kevingong2013/apkgo?arch=amd64&sort=date)

在发布应用之前请确认已经升级apkgo到最新版本，然后执行`check`命令，确认本地认证信息有效

``` shell
apkgo check
```

### 使用举例

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

## 指南

### 缘起

iOS 应用的更新发布一般都使用 App Store，而海外 Android 应用的更新发布则主要使用 Google Play Store。然而，在国内，Android 应用的更新发布则更为繁琐和复杂。华为、小米、OPPO、Vivo、魅族、联想、应用宝、百度、腾讯、360、蒲公英、fir.im 等各种应用商店和自建的发布测试服务，每次更新都需要上传多个渠道包和多个应用商店，这不仅耗费时间，而且容易出错。

因此，每个安卓开发工程师或发布经理都需要面对这个问题，apkgo的目标就是帮大家一键更新应用。

?> 后续我们将前文列出的应用市场及发布渠道统称为**商店**

!> apkgo不解决各商店的账号注册、开发这资质认证等问题

!> apkgo不解决在各商店第一次创建及发布应用的问题

!> apkgo专注于处理各商店的**应用更新**问题

### 分析问题

通过简单分析我们很容易将所有商店分为下面几类：

#### 支持API接口更新

此类商店算是比较友好的，有公开api可以通过一些简单的`http`请求完成应用更新

- 华为、小米、vivo
- 蒲公英、fir.im

#### 不支API口更新

以第三方应用商店为主，这些商店均不支持接口发布

- OPPO
- 应用宝、百度手机助手、360手机助手

#### 内部私有服务

此类一般由公司内部开发，肯定支持Api发布但是需要各个公司自己去实现。

- 自建私有服务

### 我们的目标

只要告诉`apkgo`想要发的包、更新日志、以及要更新到哪些商店他就自动完成更新发布。支持Docker，支持与主流的CI/CD集成。

### 插件

apkgo集成了插件系统，可以很方便的开发自己的更新发布流程

#### 1. 开始

使用此<https://github.com/KevinGong2013/apkgo-plugin>模版，新建仓库

查看`publisher.go`文件

``` go
type CustomPublisher struct{}

func (cp *CustomPublisher) Name() string {
 return "custom_publisher"
}

func (p *CustomPublisher) Do(req shared.PublishRequest) error {

 r := rand.Intn(10)
 time.Sleep(time.Second * time.Duration(10+r))

 if r%2 == 0 {
  return fmt.Errorf("upload %s failed", req.AppName)
 }

 return nil
}
```

只需要根据自己的业务逻辑及需求实现接口`shared.Publisher`即可

#### 2. 打包

```shell
go build . -o ./apkgo-plugin-name
```

> 可以参考apkgo项目使用 `goreleaser` 打包及发布

#### 3. 握手信息配置

打开`$APKGO_HOME/secrets/stores_config.json`文件添加如下配置

```json
{
    "stores"{
        ...
        "oppo": {},
        // key 为插件名称
        "apkgo-demo": {
            // 以下三项的至依赖于 main.go 中的配置信息
            "version": "23",
            "magic_cookie_key": "",
            "magic_cookie_value": "",

            // 团队其他成员可见用于提示安装插件
            "home_page": "kevin",
            "author": "kevin"
        }
    }
}
```

#### 4. 配置插件路径

打开`APKGO_HOME/config.yaml`文件，添加如下配置

``` yaml
# 示例忽略掉
storage:
    location: local
# 主要是以下节点的配置
plugins:
      # apkgo 插件名称
    - name: apkgo-plugin
      # 插件在本地的路径
      path: path/to/your/apk-plugin
```

#### 5. 验证

```shell
apkgo check apkgo-plugin
```

#### 6. 通知团队内其他成员配置插件
