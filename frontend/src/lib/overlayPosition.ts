// Browser event and caret rectangles are physical viewport pixels. Fixed overlays
// inherit the application's root CSS zoom and therefore need logical coordinates.
export function uiZoom(){return Number(getComputedStyle(document.documentElement).zoom)||1}
export function overlayPoint(left:number,top:number,width:number,height:number){const z=uiZoom();return {left:Math.max(8,Math.min(left,innerWidth-width*z-8))/z,top:Math.max(8,Math.min(top,innerHeight-height*z-8))/z}}
