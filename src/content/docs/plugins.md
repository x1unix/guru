+++
title = "Plugins"
description = "Extend functionality with third-party plugins"
weight = 40
draft = false
toc = true
bref = "Usage of third-party plugins to use custom actions"
+++

<h3 class="section-head" id="plugin-import"><a href="#plugin-import">Importing a plugin</a></h3>
<p>
	Before using a plugin, it should be imported into your `gilbert.yaml` file.
  Each import declaration should be in URL format

  Plugin will be download automatically at first start and you will be able to use all actions that it exports.

```yaml
plugins:
  - github://github.com/go-gilbert/gilbert-plugin-example # import URL

tasks:
  hello-world:  # each plugin action should be in format 'plugin-name:action-name'
    - action: 'example-plugin:hello-world'
      params:
        message: 'hello world'
```
</p>

<h3 class="section-head" id="import-sources"><a href="#import-sources">Import sources</a></h3>
<p>
	Each plugin import URL starts with import handler as schema (e.g.: `github://`).
  There are a few supported import sources:
</p>

<h4>Local file</h4>
<p>
  Import plugin locally by file path.

```yaml
plugins:
  - file:///home/root/path/to/plugin.so
```
</p>

<h4>Web</h4>
<p>
  Downloads plugin file from specified URL. Supported schemas are `http` and `https`.

```yaml
plugins:
  - http://example.com/storage/my_plugin.so
  - https://example.com/storage/my_plugin2.so
```
</p>

<h4>GitHub</h4>
<p>
  Plugins that are hosted on GitHub, can be downloaded by using special `github` handler.
  Handler finds specified repo and downloads latest or specified plugin release.

  Plugin artifact should be present at repo's **Releases** page.
  See [GitHub publishing](../plugin-development#publishing) for more info.

  GitHub Enterprize and token auth are also supported.
</p>
<p>
```yaml
plugins:
  - github://github.com/owner/repo_name?version=v1.0.0&token=AUTH_TOKEN
```

<b>Optional URL parameters:</b>
  <ul>
    <li>`version` - Release tag to download (default is <code>latest</code>)</li>
    <li>`token` - Your personal GitHub auth token</li>
  </ul>
</p>

<h5>GitHub Enterprise</h5>
<p>
```yaml
plugins:
  - github://company.domain.com:8888/custom_path/owner/repo_name?version=v1.0.0&token=AUTH_TOKEN
```

To use custom GitHub host, just replace `github.com` to your GitHub Enterprise instance path.

Path can contain hostname, port and path.

<b>Optional URL parameters:</b>
  <ul>
    <li>`version` - Release tag to download (default is <code>latest</code>)</li>
    <li>`token` - Your personal GitHub auth token</li>
    <li>`protocol` - Protocol to use (`http` or `https`). `https` is default value.</li>
  </ul>
</p>

<h3 class="section-head" id="explore-plugins"><a href="#explore-plugins">Explore plugins</a></h3>
<p>
  You can explore third-party plugins by searching on GitHub by <code>[gilbert-plugin](https://github.com/topics/gilbert-plugin)</code> topic.

  Also you can explore our [plugin development docs](../plugin-development) and create a plugin on your own.
</p>