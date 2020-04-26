{{define "title"}}
  tor-drop listing folder {{.Folder.Name}}
{{end}}

{{define "body"}}
  <h2>
    {{if .IsAdmin}}
    Welcome to the administrator zone
    {{else}}
    Welcome to the public zone
    {{end}}
  </h2>

  <h3>Listing folder {{.Folder.Name}}</h3>

  {{if .Error}}
    <b style="color:red">{{.Error}}</b>
    <br/>
  {{end}}

  <span>
    {{if not (.Folder.MaxFileCount | isZero)}}
      {{.Items | len}} / {{.Folder.MaxFileCount}} files
    {{end}}
    {{if not (.Folder.MaxTotalSize | isZero)}}
      {{.Items.Size | bytes}} consumed of
      {{.Folder.MaxTotalSize | bytes}} available
    {{end}}
    {{if not (.Folder.MaxFileSize | isZero)}}
      Maximum file size {{.Folder.MaxFileSize | bytes}}
    {{end}}
    {{if not (.Folder.MaxLifeTime | isZero)}}
      Maximum file lifetime {{.Folder.MaxLifeTime | durations}}
    {{end}}
  </span>

  <form method="POST" action="" enctype="multipart/form-data">
    {{$.Request | csrf}}
    Upload a file <input type="file" name="files" />
    {{if $.CaptchaID}}
    <br/>
    <img src="{{urlFor "captcha" "id" $.CaptchaID}}" />
    <input type="hidden" name="CaptchaID"value="{{$.CaptchaID}}" />
    <input type="text" name="Solution" placeholder="type in the captcha solution" />
    <br/>
    {{end}}
    <button type="submit" name="action" value="upload">send</button>
  </form>

  {{if gt (len .Items) 0}}
  <form method="post">
    {{$.Request | csrf}}
    <input type="hidden" name="action" value="rma" />
    <table>
      <tr>
        <td>Name</td>
        <td>Create date</td>
        <td>Size</td>
        <td>Uploaded</td>
        {{if .IsAdmin}}
        <td>Remove</td>
        {{end}}
      </tr>
      {{range $f := .Items}}
      <tr>
        <td><a href="{{urlFor "asset-dl" "folder" $.Folder.Name "name" $f.Name}}" target="_blank">{{$f.Name}}</a></td>
        <td>{{$f.CreateDate | times}}</td>
        <td>{{$f.Size | bytes}}</td>
        <td>{{$f.Uploaded | bytes}}</td>
        {{if $.IsAdmin}}
        <td>
          <button type="submit"
            name="Name" value="{{$f.Name}}">remove</button>
        </td>
        {{end}}
      </tr>
      {{end}}
    </table>
  </form>
  {{else}}
    This folder is currently empty!
  {{end}}


{{end}}

{{template "layout" .}}
