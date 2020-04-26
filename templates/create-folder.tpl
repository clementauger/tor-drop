{{define "title"}}
  {{if eq .action "create"}}
    tor-drop create folder
  {{end}}
  {{if eq .action "edit"}}
    tor-drop edit folder {{.Folder.Name}}
  {{end}}
  {{if eq .action "rm"}}
    tor-drop remove folder {{.Folder.Name}}
  {{end}}
{{end}}

{{define "body"}}
  <h2>
    {{if .IsAdmin}}
    Welcome to the administrator zone
    {{else}}
    Welcome to the public zone
    {{end}}
  </h2>

  {{if eq .action "create"}}
  <h3>Create a new upload folder</h3>
  {{end}}
  {{if eq .action "edit"}}
  <h3>Edit folder {{.Folder.Name}}</h3>
  {{end}}
  {{if eq .action "rm"}}
  <h3>Remove folder {{.Folder.Name}}</h3>
  {{end}}
  {{if .Error}}
    <b style="color:red">{{.Error}}</b>
  {{end}}
  <form method="POST">
    {{$.Request | csrf}}
    Name: <input type="text" name="Folder.Name" value="{{.Folder.Name}}" {{if eq .action "edit"}}readonly{{end}} />
    <br/>
    Maximum active uploads:
    <input type="text" name="Folder.MaxActiveUploads" placeholder="0 means no limit"
      value="{{.Folder.MaxActiveUploads |ints }}" />
    <br/>
    Maximum active downloads:
    <input type="text" name="Folder.MaxActiveDownloads" placeholder="0 means no limit"
      value="{{.Folder.MaxActiveDownloads |ints }}" />
    <br/>
    Maximum upload bytes per second:
      <input type="text" name="Folder.MaxUpBytesPerSec" value="{{.Folder.MaxUpBytesPerSec | bytes}}"
        placeholder="1b 250kb 1Mb" />
    <br/>
    Maximum download bytes per second:
      <input type="text" name="Folder.MaxDlBytesPerSec" value="{{.Folder.MaxDlBytesPerSec | bytes}}"
        placeholder="1b 250kb 1Mb" />
    <br/>
    Maximum files in this folder:
    <input type="text" name="Folder.MaxFileCount" placeholder="0 means no limit"
      value="{{.Folder.MaxFileCount |ints }}" />
    <br/>
    Maximum total size of this folder:
      <input type="text" name="Folder.MaxTotalSize" value="{{.Folder.MaxTotalSize | bytes}}"
        placeholder="1b 250kb 1Mb" />
    <br/>
    Maximum size per file:
      <input type="text" name="Folder.MaxFileSize" value="{{.Folder.MaxFileSize | bytes}}"
        placeholder="1b 250kb 1Mb" />
    <br/>
    Maximum file lifetime:
      <input type="text" name="Folder.MaxLifeTime" value="{{.Folder.MaxLifeTime | durations}}"
        placeholder="1m 1s 1h12m" />
    <br/>
    Is the folder private?
      <span>yes<input type="radio" name="Folder.IsPrivate" value="true"
        {{if .Folder.IsPrivate}}checked{{end}} /></span>
      <span>no<input type="radio" name="Folder.IsPrivate" value="false"
        {{if not .Folder.IsPrivate}}checked{{end}} /></span>
    <br/>
    Is the folder listable only by administrator?
      <span>yes<input type="radio" name="Folder.IsAdminOnlyReadable" value="true"
        {{if .Folder.IsAdminOnlyReadable}}checked{{end}} /></span>
      <span>no<input type="radio" name="Folder.IsAdminOnlyReadable" value="false"
        {{if not .Folder.IsAdminOnlyReadable}}checked{{end}} /></span>
    <br/>
    Require a captcha for logged users:
      <span>yes<input type="radio" name="Folder.CaptchaForLoggedUsers" value="true"
        {{if .Folder.CaptchaForLoggedUsers}}checked{{end}} /></span>
      <span>no<input type="radio" name="Folder.CaptchaForLoggedUsers" value="false"
        {{if not .Folder.CaptchaForLoggedUsers}}checked{{end}} /></span>
    <br/>
    Require a captcha for anonymous users:
      <span>yes<input type="radio" name="Folder.CaptchaForAnonymous" value="true"
        {{if .Folder.CaptchaForAnonymous}}checked{{end}} /></span>
      <span>no<input type="radio" name="Folder.CaptchaForAnonymous" value="false"
        {{if not .Folder.CaptchaForAnonymous}}checked{{end}} /></span>
    <br/>
    Require a password:
      <input type="text" name="Folder.Password" value="{{or .Folder.Password ""}}" />
    <br/>
    Add an user:
      <input type="text" placeholder="user login" name="User.Login" value="{{.User.Login}}" />
      <input type="password" placeholder="user password"name="User.Password" value="{{.User.Password}}" />
    <br/>
    {{if eq .action "create"}}
    <button type="submit" value="create" name="action">Create</button>
    {{end}}
    {{if eq .action "edit"}}
    <button type="submit" value="edit" name="action">Update</button>
    {{end}}
  </form>

  {{if gt (len .Folder.Users) 0 }}
  User list<br/>
  <table>
    <tr>
      <td>Name</td>
      <td>Password</td>
      <td>Remove</td>
    </tr>
    {{range $u,$pwds := .Folder.Users}}
    <tr>
      <td>{{$u}}</td>
      <td>{{$pwds}}</td>
      <td>
        <form method="POST">
          {{$.Request | csrf}}
          <input type="hidden" name="User" value="{{$u}}" />
          <button name="action" value="rm-user">remove</button>
        </form>
      </td>
    </tr>
    {{end}}
  </table>
  {{end}}

{{end}}

{{template "layout" .}}
