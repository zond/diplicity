package auth

import (
	"text/template"
)

var (
	messageTemplate = template.Must(template.New("").Parse(messageHTML))
)

const messageHTML = `
<html>
  <head>
    <title>{{.Title}}</title>
    <meta
      name="viewport"
      content="width=device-width, initial-scale=1, maximum-scale=1"
    />
    <link
      href="https://fonts.googleapis.com/css2?family=Roboto:wght@400;500&display=swap"
      rel="stylesheet"
    />
    <style>
      html {
		background: url(/img/message-background.jpg) no-repeat bottom right fixed;
        background-size: cover;
        font-family: "Roboto", sans-serif;
      }

      .pure-material-button-text {
        position: relative;
        display: inline-block;
        box-sizing: border-box;
        border: none;
        border-radius: 4px;
        padding: 0 16px;
        min-width: 64px;
        height: 36px;
        vertical-align: middle;
        text-align: center;
        text-overflow: ellipsis;
        text-transform: uppercase;
        color: #281a1a;
        background-color: transparent;
        font-family: "Roboto", sans-serif;

        font-size: 14px;
        font-weight: 500;
        line-height: 36px;
        overflow: hidden;
        outline: none;
        cursor: pointer;
      }

      .pure-material-button-text::-moz-focus-inner {
        border: none;
      }

      /* Overlay */
      .pure-material-button-text::before {
        content: "";
        position: absolute;
        left: 0;
        right: 0;
        top: 0;
        bottom: 0;
        background-color: currentColor;
        opacity: 0;
        transition: opacity 0.2s;
      }

      /* Ripple */
      .pure-material-button-text::after {
        content: "";
        position: absolute;
        left: 50%;
        top: 50%;
        border-radius: 50%;
        padding: 50%;
        width: 32px;
        height: 32px;
        background-color: currentColor;
        opacity: 0;
        transform: translate(-50%, -50%) scale(1);
        transition: opacity 1s, transform 0.5s;
      }

      /* Hover, Focus */
      .pure-material-button-text:hover::before {
        opacity: 0.04;
      }

      .pure-material-button-text:focus::before {
        opacity: 0.12;
      }

      .pure-material-button-text:hover:focus::before {
        opacity: 0.16;
      }

      /* Active */
      .pure-material-button-text:active::after {
        opacity: 0.16;
        transform: translate(-50%, -50%) scale(0);
        transition: transform 0s;
      }

      /* Disabled */
      .pure-material-button-text:disabled {
        color: rgba(var(--pure-material-onsurface-rgb, 0, 0, 0), 0.38);
        background-color: transparent;
        cursor: initial;
      }

      .pure-material-button-text:disabled::before {
        opacity: 0;
      }

      .pure-material-button-text:disabled::after {
        opacity: 0;
      }

      .dialog {
        box-shadow: 0px 11px 15px -7px rgba(0, 0, 0, 0.2),
          0px 24px 38px 3px rgba(0, 0, 0, 0.14),
          0px 9px 46px 8px rgba(0, 0, 0, 0.12);
        border-radius: 4px;
        background-color: white;
        box-sizing: border-box;
        max-width: 600px;
        display: flex;
        max-height: calc(100%-64px);
        flex-direction: column;
        margin: 15% 32px 32px 32px;
        overflow-y: auto;
        color: rgba(0, 0, 0, 0.87);
        font-size: 0.875rem;
        font-family: "Roboto", sans-serif;
        font-weight: 400;
        font-size: 14px;
      }

      .title {
        padding: 24px 16px;
        padding-bottom: 0px;
        font-size: 20px;
        font-weight: 500;
        line-height: 32px;
      }

      .messagetext {
        padding: 16px;
        color: rgba(0, 0, 0, 0.7);
        line-height: 20px;
      }

      .buttonlayout {
        display: flex;
        flex-direction: row-reverse;
        flex-wrap: wrap;
        padding: 0px 16px 8px 16px;
        justify-content: flex-start;
      }
    </style>
  </head>
  <body>
    <div class="dialog">
	{{.Message}}
    </div>
  </body>
</html>
`
