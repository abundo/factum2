function GenericCRUD(id) {
    const div = document.getElementById(id);
    div.innerHTML = `
<div id="crud-table" style="max-height: 100%;"></div>

<form id="crud-form" method="post">
    <div class="modal" id="crud-modal" tabindex="-1">
        <div class="modal-dialog modal-lg">
            <div class="modal-content">
                <div class="modal-header">
                    <h5 class="modal-title" id="crud-modal-title">Manage Item</h5>
                    <button type="button" class="btn-close" data-bs-dismiss="modal"></button>
                </div>
                <div class="modal-body" id="crud-modal-body"></div>
                <div class="modal-footer">
                    <button type="button" class="btn btn-link link-secondary" data-bs-dismiss="modal">Cancel</button>
                    <button type="submit" class="btn btn-primary ms-auto" id="crud-submit-btn">Save</button>
                </div>
            </div>
        </div>
    </div>
</form>
<button type="button" class="btn btn-primary mt-3" id="crud-add-btn">Add New</button>
`;
}
const mycrud = {
    ajaxURL: "",
    columns: {},
    fields: {},
    new: function () {},
};

function Mycrud(ajaxURL) {
    this.ajaxURL = ajaxURL;
}

function initGenericCRUD(config) {
    const form = document.getElementById("crud-form");
    const modal = document.getElementById("crud-modal");
    const modalBody = document.getElementById("crud-modal-body");
    const modalTitle = document.getElementById("crud-modal-title");
    const addBtn = document.getElementById("crud-add-btn");

    // -----------------------------------------------------------------------
    //  Form
    // -----------------------------------------------------------------------
    function ShowForm(id) {
        addBtn.innerText = `Add ${config.title}`;
        form.reset();
        modalBody.innerHTML = "";
        config.fields.forEach((field) => {
            switch (field.type) {
                case "hidden":
                    modalBody.insertAdjacentHTML(
                        "beforeend",
                        `<input type="hidden" name="${field.name}" id="f-${field.name}">`,
                    );
                    break;
                case "switch":
                    modalBody.insertAdjacentHTML(
                        "beforeend",
                        `
                <div class="row mb-3">
                  <label class="col-sm-4 form-label">${field.label}</label>
                  <label class="col-sm-8 form-check form-switch">
                    <input class="form-check-input"
                            type="checkbox"
                            name="${field.name}"
                            id="f-${field.name}" />
                  </label>
                </div>`,
                    );
                    break;
                case "textarea":
                    modalBody.insertAdjacentHTML(
                        "beforeend",
                        `
                <div class="row mb-3">
                    <label class="col-sm-4 form-label">${field.label}</label>
                    <div class="col-sm-8">
                      <textarea type="${field.type}" class="form-control" name="${field.name}" id="f-${field.name}" placeholder="${field.placeholder || ""}" ${field.extra} ></textarea>
                    </div>
                </div>`,
                    );
                    break;
                default:
                    // All other formats
                    modalBody.insertAdjacentHTML(
                        "beforeend",
                        `
                <div class="row mb-3">
                    <label class="col-sm-4 form-label">${field.label}</label>
                    <div class="col-sm-8">
                    <input type="${field.type}" class="form-control" name="${field.name}" id="f-${field.name}" placeholder="${field.placeholder || ""}" ${field.extra} />
                    </div>
                </div>`,
                    );
            }
        });

        // If edit, fetch data from backend
        if (id) {
            modalTitle.innerText = `Edit ${config.title}`;
            fetch(`${config.apiUrl}/${id}`)
                .then((res) => res.json())
                .then((data) => {
                    console.log("data:", data);
                    modalTitle.innerText = `Edit ${config.title}`;

                    // Auto-fill inputs based on JSON keys
                    config.fields.forEach((field) => {
                        console.log("field:", field);
                        const input = document.getElementById(`f-${field.name}`);
                        if (field.type === "switch") {
                            input.checked = !!data[field.name];
                        } else {
                            input.value = data[field.name] ?? "";
                        }
                    });
                });
        } else {
            modalTitle.innerText = `New ${config.title}`;
        }

        // AJAX SUBMIT (CREATE OR UPDATE)
        form.addEventListener("submit", function (e) {
            e.preventDefault();

            let formData = new FormData(form);
            let payload = Object.fromEntries(formData);

            // Handle unchecked switches/checkboxes (HTML ignores them in FormData if unchecked)
            config.fields.forEach((field) => {
                if (field.name == "id") {
                    payload.id = parseInt(payload.id, 10);
                } else {
                    switch (field.type) {
                        case "switch": {
                            val = document.getElementById(`f-${field.name}`).checked;
                            console.log(field.name, val);
                            payload[field.name] = val;
                            break;
                        }
                        case "number": {
                            val = document.getElementById(`f-${field.name}`).value;
                            console.log(field.name, val);
                            payload[field.name] = Number(val);
                            break;
                        }
                    }
                }
            });
            const url = payload.id ? `${config.apiUrl}/${payload.id}` : config.apiUrl;
            const method = payload.id ? "PUT" : "POST";

            fetch(url, {
                method: method,
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify(payload),
            }).then(() => {
                const m = tabler.bootstrap.Modal.getOrCreateInstance(modal);
                m.hide();
                // form.querySelector('[data-bs-dismiss="modal"]').click(); // Close Modal
                table.setData(); // Silent reload
            });
        });
        const m = tabler.bootstrap.Modal.getOrCreateInstance(modal);
        m.show();
    }

    // -----------------------------------------------------------------------
    //  Table
    // -----------------------------------------------------------------------
    //
    var showDetailIcon = function (cell, formatterParams) {
        return "<a href='#' class='btn btn-dark btn-pill btn-sm' >Detail</a>";
    };
    var openDetail = function (e, cell) {
        console.log("openService()", cell.getValue());
        ShowForm(cell.getValue());
    };

    // fix detail button
    config.columns.forEach((col) => {
        if (col.title == "ID") {
            console.log("set cellclick handler", col);
            col.formatter = showDetailIcon;
            col.cellClick = openDetail;
        }
    });

    // INITIALIZE TABULATOR
    var table = new Tabulator("#crud-table", {
        ajaxURL: config.apiUrl,
        layout: "fitData",
        id: "id",
        height: "calc(100vh - 200px)",
        columns: config.columns,
        // rowFormatter: function (row) {
        //     let el = row.getElement();
        //     el.setAttribute("data-bs-toggle", "modal");
        //     el.setAttribute("data-bs-target", "#crud-modal");
        //     el.style.cursor = "pointer";
        // },
    });
}
