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

  <h3>Login to folder {{.Folder.Name}}</h3>

  {{if .Error}}
    <b style="color:red">{{.Error}}</b>
  {{end}}

  {{if not (isZero .Folder.Password)}}
    <fieldset>
      Login with the password:
      <form method="POST" >
        {{$.Request | csrf}}
        Password <input type="password" name="Password" value="" />
        </br>
        <button type="submit" name="action" value="login">Login</button>
      </form>
    </fieldset>
  {{end}}

  {{if gt (len .Folder.Users) 0}}
  <fieldset>
    Login with your credentials:
    <form method="POST" >
      {{$.Request | csrf}}
      Login <input type="text" name="Login" value="" />
      </br>
      Password <input type="password" name="Password" value="" />
      </br>
      <button type="submit" name="action" value="userlogin">Login</button>
    </form>
  </fieldset>
  {{end}}

{{end}}

{{template "layout" .}}
