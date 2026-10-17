# 网络与代理

如果直连 API 正常，但开启代理后出现握手慢、断流、401/404 异常，先检查分流规则。

## Clash Verge Rev

1. 在 Profiles / 订阅列表右键当前订阅，选择“编辑规则”。
2. 选择“前置规则 / prepend”。
3. 添加下面这条规则，保存，模式保持 Rule：

```text
DOMAIN-SUFFIX,{{host}},DIRECT
```

4. 如果使用扩展脚本，把同样的规则插到 `rules` 最前面；不同 Clash 版本的脚本语法不同，以官方文档为准。

## 验证是否真的直连

1. 打开 Connections / 连接。
2. 访问 `{{baseUrl}}`。
3. 查看该连接的 Rule 是否命中 `DOMAIN-SUFFIX,{{host}}`，Chain 是否为 `DIRECT`。

如果仍然走代理，说明真实 Host 不是 `{{host}}`。按 Connections 里显示的 Host 再加一条规则。

## 其他代理软件

在“规则 / 分流 / 绕过代理（No Proxy）”里把 `{{host}}` 加入直连列表。

## 注意

- 不要为了“看起来直连”而关闭全部代理；只让 API 域名走 DIRECT。
- 如果站点使用了 CDN 或自定义回源域名，以浏览器/客户端实际连接的 Host 为准。
