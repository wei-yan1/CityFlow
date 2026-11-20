/* Personal templates are scoped to the authenticated account and this browser. */
window.CityFlowCustom = (() => {
  const dialog=document.querySelector('#customDialog'),form=document.querySelector('#taskForm'),host=document.querySelector('#customFormHost'),list=document.querySelector('#customSavedList'),notice=document.querySelector('#customNotice');
  const submit=form.querySelector('[type=submit]');let key='',originalParent,originalNext,originalDraft,originalFields,editingID=null;
  const read=()=>{if(!key)return [];try{const data=JSON.parse(localStorage.getItem(key)||'[]');return Array.isArray(data)?data:[]}catch{return []}};
  function fields(){return {title:form.elements.title.value,type:form.elements.type.value,description:form.elements.description.value}}
  function setFields(data){form.elements.title.value=data.title||'';form.elements.type.value=data.type||'DELIVERY';form.elements.description.value=data.description||''}
  function nowDraft(draft){const value=structuredClone(draft);value.stops.forEach(stop=>stop.pickup_at=null);value.deliveryAt=null;return value}
  function render(){list.replaceChildren();const items=read();if(!items.length){const p=document.createElement('p');p.textContent='还没有个人定制，请在下方添加。';list.append(p);return}
    for(const item of items){const row=document.createElement('div');row.className='custom-record';const label=document.createElement('span');label.textContent=item.fields.title;const edit=document.createElement('button');edit.type='button';edit.className='button outline';edit.textContent='编辑';edit.addEventListener('click',()=>{editingID=item.id;setFields(item.fields);CityFlowTaskCompose.setDraft(nowDraft(item.draft));notice.textContent='正在编辑定制。'});const remove=document.createElement('button');remove.type='button';remove.className='button outline';remove.textContent='删除';remove.addEventListener('click',()=>{try{localStorage.setItem(key,JSON.stringify(read().filter(v=>v.id!==item.id)));if(editingID===item.id){editingID=null;form.reset()}render();notice.textContent='已删除定制。'}catch{notice.textContent='删除失败，请检查浏览器存储设置。'}});const apply=document.createElement('button');apply.type='button';apply.className='button primary';apply.textContent='应用任务';apply.addEventListener('click',()=>{const saved=item;close();setFields(saved.fields);CityFlowTaskCompose.setDraft(nowDraft(saved.draft));showNotice('已应用常用任务，请确认后发布。','success')});row.append(label,apply,edit,remove);list.append(row)}
  }
  function restore(){if(!originalParent)return;originalParent.insertBefore(form,originalNext);submit.textContent='发布任务';setFields(originalFields);CityFlowTaskCompose.setDraft(originalDraft);originalParent=null;editingID=null}
  function close(){if(!dialog.open)return;restore();dialog.close()}
  dialog.addEventListener('cancel',restore);dialog.addEventListener('close',restore);
  document.querySelector('[data-close-custom]').addEventListener('click',close);
  document.querySelector('#openCustom').addEventListener('click',()=>{if(!key)return;originalParent=form.parentNode;originalNext=form.nextSibling;originalDraft=CityFlowTaskCompose.draft();originalFields=fields();editingID=null;host.append(form);form.reset();submit.textContent='保存定制';notice.textContent='';render();dialog.showModal()});
  return {
    initialize(id){key=`cityflow:custom:${id}`},
    isEditing(){return Boolean(originalParent)},
    save(){const draft=CityFlowTaskCompose.draft();if(draft.stops.some(stop=>!stop.address||!stop.location)||!draft.destination?.location){notice.textContent='请选择所有取件和送达地点。';return}const normalized=nowDraft(draft);normalized.stops.forEach(stop=>stop.pickup_at=null);const item={id:editingID||crypto.randomUUID(),fields:fields(),draft:normalized};try{const items=read();const index=items.findIndex(value=>value.id===item.id);if(index<0)items.push(item);else items[index]=item;localStorage.setItem(key,JSON.stringify(items));editingID=item.id;render();notice.textContent='定制已保存。'}catch{notice.textContent='保存失败，请检查浏览器存储空间。'}},
    places(){return read().flatMap(item=>[...item.draft.stops,item.draft.destination].filter(place=>place?.address&&place.location))},
  };
})();
