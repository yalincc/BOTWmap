/*
 * botw-icons.js — BOTW 物品图标精灵映射（从 marcrobledo 抽取）
 * 渲染用 CSS background-position；精灵 PNG 在 ./assets/。
 * iconStyle(id, size) 按目标尺寸计算缩放，避免 44px 容器裁切 64px 图。
 */
(function (root, factory) {
  if (typeof module !== 'undefined' && module.exports) module.exports = factory();
  else root.BOTW_ICONS = factory();
})(typeof self !== 'undefined' ? self : this, function () {
  'use strict';
  const ICONS = {
    "Weapon_Sword": ["001","002","003","004","005","006","007","008","009","013","014","015","016","017","018","019","020","021","022","023","024","025","027","029","030","031","033","034","035","040","041","043","044","047","048","049","050","051","052","053","056","057","058","059","060","061","062","070","071","072","073","500","501","502"],
    "Weapon_Lsword": ["001","002","003","004","005","006","010","011","012","013","014","015","016","017","018","019","020","023","024","027","029","030","031","032","033","034","035","036","037","038","041","045","047","051","054","055","056","057","059","060","074"],
    "Weapon_Spear": ["001","002","003","004","005","006","007","008","009","010","011","012","013","014","015","016","017","018","021","022","023","024","025","027","028","029","030","031","032","033","034","035","036","037","038","047","049","050"],
    "Weapon_Bow": ["001","002","003","004","006","009","011","013","014","015","016","017","023","026","027","028","029","030","032","033","035","036","038","040","071","072"],
    "Weapon_Shield": ["001","002","003","004","005","006","007","008","009","013","014","015","016","017","018","021","022","023","025","026","030","031","032","033","034","035","036","037","038","040","041","042","057"],
    "Item_Fruit": ["A","B","C","D","E","F","G","H","I","J","K","L"],
    "Item_MushroomGet": ["A","B","C","D","E","F","H","J","L","M","N","O"],
    "Item_InsectGet": ["A","AA","AB","B","C","E","F","G","H","I","K","M","N","O","P","Q","R","S","T","X","Z","NormalK","NormalO","NormalZ"],
    "Item_FishGet": ["A","B","C","D","E","F","G","H","I","J","K","L","M","X","Z"],
    "Item_Ore": ["A","B","C","D","E","F","G","H","I","J"],
    "Item_PlantGet": ["A","B","C","E","F","G","H","I","J","L","M","O","Q"],
    "Item_Meat": ["01","02","06","07","11","12"],
    "Item_Roast": ["01","02","03","04","05","06","07","08","09","10","11","12","13","15","16","18","19","24","27","28","31","32","33","36","37","38","39","40","41","45","46","48","49","50","51","52","53"],
    "Item_RoastFish": ["01","02","03","04","07","09","11","13","15"],
    "Item_Chilled": ["01","02","03","04","05","06"],
    "Item_ChilledFish": ["01","02","03","04","05","06","07","08","09"],
    "Item_Enemy": ["00","01","02","03","04","05","06","07","08","12","13","14","15","16","17","18","19","20","21","24","25","26","27","28","29","30","31","32","33","34","38","39","40","41","42","43","44","45","46","47","48","49","50","51","52","53","54","55","56","57","Put_57"],
    "Item_Material": ["01","02","03","04","05","06","07","08"],
    "Item_Cook": ["A_01","A_02","A_03","A_04","A_05","A_07","A_08","A_09","A_10","A_11","A_12","A_13","A_14","B_01","B_02","B_05","B_06","B_11","B_12","B_13","B_15","B_16","B_17","B_18","B_19","B_20","B_21","B_22","B_23","C_16","C_17","C_17_Mod01","C_17_Mod02","C_17_Mod03","C_17_Mod04","C_17_Mod05","C_17_Mod06","C_17_Mod07","C_17_Mod08","C_17_Mod09","C_17_Mod10","D_01","D_02","D_03","D_04","D_05","D_06","D_07","D_08","D_09","D_10","E_01","E_02","E_03","E_04","F_01","F_02","F_03","F_04","G_02","G_03","G_04","G_05","G_06","G_09","G_10","G_11","G_12","G_13","G_14","G_15","G_16","G_17","H_01","H_02","H_03","I_01","I_02","I_03","I_04","I_05","I_06","I_07","I_08","I_09","I_10","I_11","I_12","I_13","I_14","I_15","I_16","I_17","J_01","J_02","J_03","J_04","J_05","J_06","J_07","J_08","J_09","K_01","K_02","K_03","K_04","K_05","K_06","K_07","K_08","K_09","L_01","L_02","L_03","L_04","L_05","M_01","N_01","N_02","N_03","N_04","O_01","O_02","P_01","P_02","P_03","P_04","P_05"],
    // Other.png 前两行：染料 + 箭矢；只取到 AncientArrow，坐标为原始 sheet 顺序
    "Other": ["_none","Dye01","Dye02","Dye03","Dye04","Dye05","Dye06","Dye07","Dye08","Dye09","Dye10","Dye11","Dye12","Dye13","Dye14","Dye15","NormalArrow","FireArrow","IceArrow","ElectricArrow","BombArrow_A","AncientArrow"]
  };
  const ICON_COLS = 16;
  const ICON_SIZE = 64;

  function resolveIcon(id) {
    var fileName, idx, match;
    if (match = id.match(/^Weapon_(Sword|Lsword|Spear|Bow|Shield)_([0-9]{3})/)) { fileName = 'Weapon_' + match[1]; idx = match[2]; }
    else if (match = id.match(/^Item_(Chilled|ChilledFish|Enemy|Material|Meat|Roast|RoastFish)_([0-9]{2})/)) { fileName = 'Item_' + match[1]; idx = match[2]; }
    else if (id === 'Item_Enemy_Put_57') { fileName = 'Item_Enemy'; idx = 'Put_57'; }
    else if (match = id.replace('Animal_Insect_', 'Item_InsectGet_').replace('Mushroom_', 'MushroomGet_').replace('Plant_', 'PlantGet_').match(/^Item_(FishGet|Fruit|InsectGet|MushroomGet|Ore|PlantGet)_([A-Z][A-B]?)/)) { fileName = 'Item_' + match[1]; idx = match[2]; }
    else if (match = id.match(/^Item_Cook_([A-P]_[0-9]{2}(?:_Mod[0-9]{2})?)/)) { fileName = 'Item_Cook'; idx = match[1]; }
    else if (ICONS.Other && ICONS.Other.indexOf(id) >= 0) { fileName = 'Other'; idx = id; }
    else return null;

    var list = ICONS[fileName];
    if (!list) return null;
    var i = list.indexOf(idx);
    if (i < 0) return null;
    return { file: fileName, index: i };
  }

  // 返回 CSS background 样式（按 size 缩放精灵）
  function iconStyle(id, size) {
    var r = resolveIcon(id);
    if (!r) return null;
    size = size || 48;
    var x = (r.index % ICON_COLS) * size;
    var y = Math.floor(r.index / ICON_COLS) * size;
    return {
      backgroundImage: 'url(assets/' + r.file + '.png)',
      backgroundPosition: '-' + x + 'px -' + y + 'px',
      backgroundRepeat: 'no-repeat',
      width: size + 'px',
      height: size + 'px',
      backgroundSize: (ICON_COLS * size) + 'px auto'
    };
  }

  return { ICONS: ICONS, ICON_COLS: ICON_COLS, ICON_SIZE: ICON_SIZE, resolveIcon: resolveIcon, iconStyle: iconStyle };
});
