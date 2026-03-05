import { defineCustomElement } from 'vue'
import FlumeViewer from './FlumeViewer.ce.vue'

const FlumeViewerElement = defineCustomElement(FlumeViewer)
customElements.define('flume-viewer', FlumeViewerElement)
