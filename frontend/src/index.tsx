import { render } from "preact";

import "./style.css";

export function App() {
  return null;
}

const appEntryPoint = document.createElement("div");

document.body.appendChild(appEntryPoint);

render(<App />, appEntryPoint);
