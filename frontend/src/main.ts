import "./app.css";
import { mount } from "svelte";
import App from "./App.svelte";
import { initTheme } from "./lib/theme";

initTheme(); // apply the persisted/OS theme before mount so there's no flash

const app = mount(App, { target: document.getElementById("app")! });

export default app;
