<!--
    Example Icinga notification email template for factum-icinga-notifications
    (cmd/icinga-notifications). Copy this to the path passed as
    --template-file (default /etc/factum2/icinga-notification-email.tpl) and
    adjust as desired.

    This is a Go html/template - every interpolated field is HTML-escaped
    automatically, and the "nl2br" function (registered by the binary)
    safely turns a multi-line field into <br>-separated, escaped lines.
    See cmd/icinga-notifications/factum-icinga-notifications.go's emailData
    struct for every field available here.
-->
<!DOCTYPE html>
<html>
  <head>
    <meta charset="utf-8">
  </head>
  <body style="font-family: sans-serif; font-size: 14px;">
    {{if .IsService}}
      {{.NotificationType}}, Host <strong>{{.HostName}}</strong>, Service <strong>{{.ServiceDisplayName}}</strong> is in state <strong>{{.ServiceState}}</strong><br>
    {{else}}
      {{.NotificationType}}, Host <strong>{{.HostName}}</strong> is in state <strong>{{.HostState}}</strong><br>
    {{end}}
    <br>

    <table style="border-collapse: collapse;">
      <tr style="border-top: 1px solid black;">
        <td style="padding: 0.2em 0.5em;" colspan="2"><strong>Alarm</strong></td>
      </tr>
      <tr style="border-top: 1px solid black; vertical-align:top;">
        <td style="padding: 0.2em 0.5em;">Info</td>
        <td style="padding: 0.2em 0.5em;">{{.Info}}</td>
      </tr>
      {{if .When}}
      <tr style="border-top: 1px solid black; vertical-align:top;">
        <td style="padding: 0.2em 0.5em;">When</td>
        <td style="padding: 0.2em 0.5em;">{{.When}}</td>
      </tr>
      {{end}}
      {{if .NotificationAuthor}}
      <tr style="border-top: 1px solid black; vertical-align:top;">
        <td style="padding: 0.2em 0.5em;">Notification comment by</td>
        <td style="padding: 0.2em 0.5em;">{{.NotificationAuthor}}</td>
      </tr>
      {{end}}
      {{if .NotificationComment}}
      <tr style="border-top: 1px solid black; vertical-align:top;">
        <td style="padding: 0.2em 0.5em;">Notification comment</td>
        <td style="padding: 0.2em 0.5em;">{{.NotificationComment | nl2br}}</td>
      </tr>
      {{end}}

      {{if not .IsService}}
      <tr style="border-top: 1px solid black;">
        <td style="padding: 0.2em 0.5em;" colspan="2"><strong>Hardware</strong></td>
      </tr>
      <tr style="border-top: 1px solid black; vertical-align:top;">
        <td style="padding: 0.2em 0.5em;">Host</td>
        <td style="padding: 0.2em 0.5em;">{{.HostName}}</td>
      </tr>
      {{if .Location}}
      <tr style="border-top: 1px solid black; vertical-align:top;">
        <td style="padding: 0.2em 0.5em;">Location</td>
        <td style="padding: 0.2em 0.5em;">{{.Location}}</td>
      </tr>
      {{end}}
      {{if .SiteName}}
      <tr style="border-top: 1px solid black; vertical-align:top;">
        <td style="padding: 0.2em 0.5em;">Site name</td>
        <td style="padding: 0.2em 0.5em;">{{.SiteName}}</td>
      </tr>
      {{end}}
      {{if .Parents}}
      <tr style="border-top: 1px solid black; vertical-align:top;">
        <td style="padding: 0.2em 0.5em;">Parents</td>
        <td style="padding: 0.2em 0.5em;">{{.Parents}}</td>
      </tr>
      {{end}}
      {{if .Role}}
      <tr style="border-top: 1px solid black; vertical-align:top;">
        <td style="padding: 0.2em 0.5em;">Role</td>
        <td style="padding: 0.2em 0.5em;">{{.Role}}</td>
      </tr>
      {{end}}
      {{if .Manufacturer}}
      <tr style="border-top: 1px solid black; vertical-align:top;">
        <td style="padding: 0.2em 0.5em;">Manufacturer</td>
        <td style="padding: 0.2em 0.5em;">{{.Manufacturer}}</td>
      </tr>
      {{end}}
      {{if .Model}}
      <tr style="border-top: 1px solid black; vertical-align:top;">
        <td style="padding: 0.2em 0.5em;">Model</td>
        <td style="padding: 0.2em 0.5em;">{{.Model}}</td>
      </tr>
      {{end}}
      {{if .HostAddress}}
      <tr style="border-top: 1px solid black; vertical-align:top;">
        <td style="padding: 0.2em 0.5em;">IPv4</td>
        <td style="padding: 0.2em 0.5em;">{{.HostAddress}}</td>
      </tr>
      {{end}}
      {{if .HostAddress6}}
      <tr style="border-top: 1px solid black; vertical-align:top;">
        <td style="padding: 0.2em 0.5em;">IPv6</td>
        <td style="padding: 0.2em 0.5em;">{{.HostAddress6}}</td>
      </tr>
      {{end}}

      {{if or .Comments .Platform}}
      <tr style="border-top: 1px solid black;">
        <td style="padding: 0.2em 0.5em;" colspan="2"><strong>Other</strong></td>
      </tr>
      {{if .Comments}}
      <tr style="border-top: 1px solid black; vertical-align:top;">
        <td style="padding: 0.2em 0.5em;">Comments</td>
        <td style="padding: 0.2em 0.5em;">{{.Comments | nl2br}}</td>
      </tr>
      {{end}}
      {{if .Platform}}
      <tr style="border-top: 1px solid black; vertical-align:top;">
        <td style="padding: 0.2em 0.5em;">Platform</td>
        <td style="padding: 0.2em 0.5em;">{{.Platform}}</td>
      </tr>
      {{end}}
      {{end}}
      {{end}}

      {{if .IcingaLink}}
      <tr style="border-top: 1px solid black; vertical-align:top;">
        <td style="padding: 0.2em 0.5em;">Link</td>
        <td style="padding: 0.2em 0.5em;"><a href="{{.IcingaLink}}">{{.IcingaLinkLabel}}</a></td>
      </tr>
      {{end}}
    </table>

    <br><strong>Hosts - not acknowledged</strong><br>
    {{if .HostsDownError}}
      Error getting list of down hosts: {{.HostsDownError}}<br>
    {{else if .HostsDown}}
      Number of hosts: {{len .HostsDown}}<br>
      Approximate number of customers down: {{.CustomersDownEstimate}}<br>
      <table style="border-collapse: collapse;">
        <tr style="border-top: 1px solid black;">
          <th style="padding: 0.2em 0.5em; text-align:left;">Host</th>
          <th style="padding: 0.2em 0.5em;">Time</th>
          <th style="padding: 0.2em 0.5em;">Changed</th>
          <th style="padding: 0.2em 0.5em; text-align:left;">Location</th>
          <th style="padding: 0.2em 0.5em; text-align:left;">Role</th>
          <th style="padding: 0.2em 0.5em; text-align:left;">Manufacturer</th>
          <th style="padding: 0.2em 0.5em; text-align:left;">Model</th>
          <th style="padding: 0.2em 0.5em; text-align:left;">Notes</th>
        </tr>
        {{range .HostsDown}}
        <tr style="border-top: 1px solid black; vertical-align:top;">
          <td style="padding: 0.2em 0.5em;">{{.Name}}</td>
          <td style="padding: 0.2em 0.5em; text-align:right;">{{.Since}}</td>
          <td style="padding: 0.2em 0.5em;">{{.Changed}}</td>
          <td style="padding: 0.2em 0.5em;">{{.Location}}</td>
          <td style="padding: 0.2em 0.5em;">{{.Role}}</td>
          <td style="padding: 0.2em 0.5em;">{{.Manufacturer}}</td>
          <td style="padding: 0.2em 0.5em;">{{.Model}}</td>
          <td style="padding: 0.2em 0.5em;">{{.Notes}}</td>
        </tr>
        {{end}}
      </table>
    {{else}}
      None<br>
    {{end}}

    <br><strong>Services - not acknowledged</strong><br>
    {{if .ServicesDownError}}
      Error getting list of down services: {{.ServicesDownError}}<br>
    {{else if .ServicesDown}}
      Number of services: {{len .ServicesDown}}<br>
      <table style="border-collapse: collapse;">
        <tr style="border-top: 1px solid black;">
          <th style="padding: 0.2em 0.5em; text-align:left;">Host</th>
          <th style="padding: 0.2em 0.5em; text-align:left;">Service</th>
          <th style="padding: 0.2em 0.5em;">Time</th>
          <th style="padding: 0.2em 0.5em;">Changed</th>
          <th style="padding: 0.2em 0.5em; text-align:left;">Output</th>
          <th style="padding: 0.2em 0.5em; text-align:left;">Notes</th>
        </tr>
        {{range .ServicesDown}}
        <tr style="border-top: 1px solid black; vertical-align:top;">
          <td style="padding: 0.2em 0.5em;">{{.Host}}</td>
          <td style="padding: 0.2em 0.5em;">{{.Service}}</td>
          <td style="padding: 0.2em 0.5em; text-align:right;">{{.Since}}</td>
          <td style="padding: 0.2em 0.5em;">{{.Changed}}</td>
          <td style="padding: 0.2em 0.5em;">{{.Output}}</td>
          <td style="padding: 0.2em 0.5em;">{{.Notes}}</td>
        </tr>
        {{end}}
      </table>
    {{else}}
      None<br>
    {{end}}
  </body>
</html>
