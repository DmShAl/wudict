
// Group membership never writes disabled, prefOrder or the search selector.
let userGroups=[], selectedGroup="all", groupSaving=false, groupShowAllPreferred=false;
let pickerGroup=localStorage.getItem("wudict_picker_group")||"all";
async function loadPickerGroups(){
  try{userGroups=await groupRequest("/api/groups","GET");
    if(!userGroups.some(g=>g.id===pickerGroup))pickerGroup="all";
  }catch(e){console.warn("could not load dictionary groups:",e);pickerGroup="all"}
}
async function groupRequest(path,method,body){
  const response=await fetch(path,{method,headers:{"Content-Type":"application/json"},body:body===undefined?undefined:JSON.stringify(body)});
  if(!response.ok)throw new Error((await response.text()).trim());
  return response.json();
}
function renderGroupOptions(){
  const select=$("groupSelect");select.replaceChildren();
  for(const group of userGroups)select.add(new Option(group.name,group.id));
  select.add(new Option("New Group","new"));select.value=selectedGroup;
}
function groupHint(group,showAll,ordered){
  const members=new Set(group.members);
  const inside=ordered.filter(d=>members.has(d.id)).length;
  const outside=ordered.length-inside;
  return group.readonly?"Drag ≡ to reorder all dictionaries."
    :!showAll?(inside?"Drag ≡ to reorder. Turn on Show All to edit membership.":"This group is empty. Turn on Show All to add dictionaries.")
    :inside?"":outside?"Select dictionaries to add to this group.":"No dictionaries available. Add dictionaries to your collection first.";
}
function renderGroupRows(){
  const group=userGroups.find(g=>g.id===selectedGroup), rows=$("groupRows");rows.replaceChildren();
  if(!group)return;
  const showAll=$("groupShowAll");
  showAll.disabled=group.readonly;
  showAll.checked=group.readonly||groupShowAllPreferred;
  showAll.parentElement.classList.toggle("group-control-disabled",group.readonly);
  const ordered=orderedDicts(), members=new Set(group.readonly?ordered.map(d=>d.id):group.members);
  const inside=group.readonly?ordered:orderedGroupDicts(group,ordered);
  const outside=group.readonly?[]:ordered.filter(d=>!members.has(d.id));
  const visible=showAll.checked?inside.concat(outside):inside;
  $("groupHint").textContent=groupHint(group,showAll.checked,ordered);
  for(const d of visible){
    const label=document.createElement(!group.readonly&&showAll.checked?"label":"div");label.className="group-row";
    if(showAll.checked&&inside.length&&d===outside[0])label.classList.add("group-other-first");
    label.dataset.dict=d.id;
    const name=document.createElement("span");name.textContent=dictLabel(d);
    if(group.readonly||!showAll.checked){
      const grip=document.createElement("button");grip.type="button";grip.className="group-grip";
      grip.textContent="≡";grip.setAttribute("aria-label","Drag to reorder "+dictLabel(d));
      label.append(grip,name);rows.append(label);continue;
    }
    const checkbox=document.createElement("input");checkbox.type="checkbox";checkbox.dataset.dict=d.id;checkbox.checked=members.has(d.id);checkbox.disabled=group.readonly||groupSaving;
    label.append(checkbox,name);rows.append(label);
    checkbox.addEventListener("change",async()=>{
      const member=checkbox.checked,scrollTop=rows.scrollTop;groupSaving=true;$("groupError").textContent="";
      $("closeGroups").disabled=true;$("groupSelect").disabled=true;showAll.disabled=true;
      rows.querySelectorAll("input").forEach(input=>input.disabled=true);
      try{
        await groupRequest("/api/groups/member","PUT",{group:group.id,dict:d.id,member});
        group.members=group.members.filter(id=>id!==d.id);if(member)group.members.push(d.id);
        refreshLivePicker();if(group.id===pickerGroup&&$("q").value.trim())doSearch();
      }catch(error){$("groupError").textContent=error.message}
      finally{
        groupSaving=false;$("closeGroups").disabled=false;$("groupSelect").disabled=false;showAll.disabled=group.readonly;
        renderGroupRows();
        rows.scrollTop=scrollTop;
      }
    });
  }
}
async function saveGroupOrder(ids,focusId){
  if(groupSaving)return;
  const group=userGroups.find(g=>g.id===selectedGroup);
  if(!group||(!group.readonly&&$("groupShowAll").checked))return;
  const rows=$("groupRows"),top=rows.scrollTop;
  groupSaving=true;$("groupError").textContent="";
  $("closeGroups").disabled=true;$("groupSelect").disabled=true;$("groupShowAll").disabled=true;
  try{
    if(group.readonly){
      prefOrder=ids;savePrefs(true);refreshDictUI(true);
    }else{
      await groupRequest("/api/groups/order","PUT",{group:group.id,members:ids});
      group.members=ids;
    }
    refreshLivePicker();
    if(group.id===pickerGroup&&$("q").value.trim())doSearch();
  }catch(error){$("groupError").textContent=error.message}
  finally{
    groupSaving=false;$("closeGroups").disabled=false;$("groupSelect").disabled=false;
    renderGroupRows();rows.scrollTop=top;
    if(focusId){const grip=[...rows.querySelectorAll(".group-row")].find(row=>row.dataset.dict===focusId)?.querySelector(".group-grip");grip?.focus({preventScroll:true})}
  }
}
let groupDrag=null;
function clearGroupDrop(){
  $("groupRows").querySelectorAll(".dragging,.drop-before,.drop-after").forEach(row=>
    row.classList.remove("dragging","drop-before","drop-after"));
}
function updateGroupDrop(){
  if(!groupDrag)return;
  const drag=groupDrag,rows=$("groupRows");
  const row=document.elementFromPoint(drag.x,drag.y)?.closest(".group-row");
  rows.querySelectorAll(".drop-before,.drop-after").forEach(el=>el.classList.remove("drop-before","drop-after"));
  drag.target=null;
  if(!row||!rows.contains(row)||row.dataset.dict===drag.id)return;
  drag.target=row.dataset.dict;
  drag.after=drag.y>row.getBoundingClientRect().top+row.getBoundingClientRect().height/2;
  row.classList.add(drag.after?"drop-after":"drop-before");
}
function groupDragTick(){
  if(!groupDrag)return;
  const rows=$("groupRows"),rect=rows.getBoundingClientRect(),edge=44;
  if(groupDrag.y<rect.top+edge)rows.scrollTop-=Math.min(28,Math.max(0,(rect.top+edge-groupDrag.y)/5));
  else if(groupDrag.y>rect.bottom-edge)rows.scrollTop+=Math.min(28,Math.max(0,(groupDrag.y-rect.bottom+edge)/5));
  updateGroupDrop();
  groupDrag.raf=requestAnimationFrame(groupDragTick);
}
$("groupRows").addEventListener("pointerdown",event=>{
  const grip=event.target.closest(".group-grip");
  if(!grip||groupSaving||event.button!==0)return;
  event.preventDefault();
  const row=grip.closest(".group-row");
  groupDrag={id:row.dataset.dict,x:event.clientX,y:event.clientY,pointerId:event.pointerId,target:null,after:false,raf:null};
  row.classList.add("dragging");
  grip.setPointerCapture(event.pointerId);
  groupDrag.raf=requestAnimationFrame(groupDragTick);
});
$("groupRows").addEventListener("pointermove",event=>{
  if(!groupDrag||event.pointerId!==groupDrag.pointerId)return;
  event.preventDefault();groupDrag.x=event.clientX;groupDrag.y=event.clientY;updateGroupDrop();
});
function finishGroupDrag(event,cancel){
  if(!groupDrag||event.pointerId!==groupDrag.pointerId)return;
  groupDrag.x=event.clientX;groupDrag.y=event.clientY;updateGroupDrop();
  const drag=groupDrag;groupDrag=null;cancelAnimationFrame(drag.raf);clearGroupDrop();
  if(cancel||!drag.target)return;
  const group=userGroups.find(g=>g.id===selectedGroup);if(!group)return;
  const current=group.readonly?orderedIds():group.members.slice();
  const ids=current.slice(),from=ids.indexOf(drag.id);if(from<0)return;
  ids.splice(from,1);
  let to=ids.indexOf(drag.target);if(to<0)return;
  if(drag.after)to++;
  ids.splice(to,0,drag.id);
  if(ids.some((id,i)=>id!==current[i]))saveGroupOrder(ids,drag.id);
}
$("groupRows").addEventListener("pointerup",event=>finishGroupDrag(event,false));
$("groupRows").addEventListener("pointercancel",event=>finishGroupDrag(event,true));
$("groupRows").addEventListener("keydown",event=>{
  if(!event.target.classList.contains("group-grip")||!(["ArrowUp","ArrowDown"].includes(event.key)))return;
  const group=userGroups.find(g=>g.id===selectedGroup);if(!group||groupSaving)return;
  const id=event.target.closest(".group-row").dataset.dict,ids=group.readonly?orderedIds():group.members.slice(),from=ids.indexOf(id);
  const to=from+(event.key==="ArrowUp"?-1:1);
  if(from<0||to<0||to>=ids.length)return;
  event.preventDefault();ids.splice(from,1);ids.splice(to,0,id);saveGroupOrder(ids,id);
});
$("editGroups").addEventListener("click",async()=>{
  const button=$("editGroups");button.disabled=true;
  $("groupError").textContent="";$("groupHint").textContent="Loading…";$("groupRows").replaceChildren();
  $("groupSelect").disabled=true;$("groupEditor").showModal();
  try{
    userGroups=await groupRequest("/api/groups","GET");refreshLivePicker();
    if(!userGroups.some(g=>g.id===selectedGroup))selectedGroup="all";
    renderGroupOptions();renderGroupRows();$("groupSelect").disabled=false;
  }catch(error){$("groupError").textContent=error.message;$("groupHint").textContent=""}
  finally{button.disabled=false}
});
$("closeGroups").onclick=()=>$("groupEditor").close();
$("groupEditor").addEventListener("close",()=>$("editGroups").focus());
$("groupEditor").addEventListener("cancel",event=>{if(groupSaving)event.preventDefault()});
$("groupShowAll").onchange=()=>{groupShowAllPreferred=$("groupShowAll").checked;renderGroupRows()};
$("groupSelect").onchange=()=>{
  if($("groupSelect").value==="new"){
    $("groupSelect").value=selectedGroup;$("groupName").value="";$("newGroupError").textContent="";
    $("newGroupDialog").showModal();$("groupName").focus();
  }else{selectedGroup=$("groupSelect").value;$("groupError").textContent="";renderGroupRows()}
};
$("cancelGroup").onclick=()=>$("newGroupDialog").close();
$("newGroupDialog").addEventListener("close",()=>$("groupSelect").focus());
$("newGroupForm").onsubmit=async event=>{
  event.preventDefault();if($("createGroup").disabled)return;
  $("createGroup").disabled=true;$("cancelGroup").disabled=true;$("newGroupError").textContent="";
  try{
    const group=await groupRequest("/api/groups","POST",{name:$("groupName").value});
    userGroups.push(group);selectedGroup=group.id;renderGroupOptions();renderGroupRows();refreshLivePicker();$("newGroupDialog").close();
  }catch(error){$("newGroupError").textContent=error.message}
  finally{$("createGroup").disabled=false;$("cancelGroup").disabled=false}
};
$("newGroupDialog").addEventListener("cancel",event=>{if($("createGroup").disabled)event.preventDefault()});
Promise.all([loadPrefs(),loadConfig(),loadUserCSS(),loadPickerGroups()]).then(loadDicts).then(applyURL);
// The preset layers are independent of the boot chain: their app halves are
// server-injected <link>s and their article halves join the article sheet
// whenever this answer lands, which is why this is fire-and-forget rather
// than a gate on the first search.
presetsLoad();
